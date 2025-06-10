package aws

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	aws3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
	"github.com/openimsdk/tools/s3"
)

const (
	minPartSize int64 = 1024 * 1024 * 5        // 1MB
	maxPartSize int64 = 1024 * 1024 * 1024 * 5 // 5GB
	maxNumSize  int64 = 10000
)

type Config struct {
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Endpoint        string // 自定义端点，用于支持R2等S3兼容服务
	BucketURL       string // 自定义bucket URL，用于特殊的访问模式
	PublicRead      bool   // 是否启用公开读取模式，当此项为true时，AccessURL将返回直接访问URL而非签名URL
}

func NewAws(conf Config) (*Aws, error) {
	cfg := aws.Config{
		Region:      conf.Region,
		Credentials: credentials.NewStaticCredentialsProvider(conf.AccessKeyID, conf.SecretAccessKey, conf.SessionToken),
	}

	client := aws3.NewFromConfig(cfg, func(o *aws3.Options) {
		// 如果指定了自定义端点（如R2），则使用自定义resolver和路径风格访问
		if conf.Endpoint != "" {
			o.EndpointResolverV2 = &customEndpointResolver{endpoint: conf.Endpoint}
			o.UsePathStyle = true
			// 确保使用指定的region，而不是从endpoint推断
			if conf.Region != "" {
				o.Region = conf.Region
			}
		}
	})

	return &Aws{
		bucket:     conf.Bucket,
		client:     client,
		presign:    aws3.NewPresignClient(client),
		bucketURL:  conf.BucketURL,
		publicRead: conf.PublicRead,
	}, nil
}

type Aws struct {
	bucket     string
	client     *aws3.Client
	presign    *aws3.PresignClient
	bucketURL  string
	publicRead bool
}

func (a *Aws) Engine() string {
	return "aws"
}

func (a *Aws) PartLimit() (*s3.PartLimit, error) {
	return &s3.PartLimit{
		MinPartSize: minPartSize,
		MaxPartSize: maxPartSize,
		MaxNumSize:  maxNumSize,
	}, nil
}

func (a *Aws) formatETag(etag string) string {
	return strings.Trim(etag, `"`)
}

func (a *Aws) PartSize(ctx context.Context, size int64) (int64, error) {
	if size <= 0 {
		return 0, errors.New("size must be greater than 0")
	}
	if size > maxPartSize*maxNumSize {
		return 0, fmt.Errorf("aws size must be less than the maximum allowed limit")
	}
	if size <= minPartSize*maxNumSize {
		return minPartSize, nil
	}
	partSize := size / maxNumSize
	if size%maxNumSize != 0 {
		partSize++
	}
	return partSize, nil
}

func (a *Aws) IsNotFound(err error) bool {
	var respErr *awshttp.ResponseError
	if !errors.As(err, &respErr) {
		return false
	}
	if respErr == nil || respErr.Response == nil {
		return false
	}
	return respErr.Response.StatusCode == http.StatusNotFound
}

func (a *Aws) PresignedPutObject(ctx context.Context, name string, expire time.Duration, opt *s3.PutOption) (*s3.PresignedPutResult, error) {
	res, err := a.presign.PresignPutObject(ctx, &aws3.PutObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(name)}, aws3.WithPresignExpires(expire), withDisableHTTPPresignerHeaderV4(nil))
	if err != nil {
		return nil, err
	}

	resultURL := res.URL
	// 如果配置了自定义的bucketURL（用于R2等服务的特殊访问模式），则替换URL的host部分
	if a.bucketURL != "" {
		u, err := url.Parse(res.URL)
		if err != nil {
			return nil, err
		}
		bucketU, err := url.Parse(a.bucketURL)
		if err != nil {
			return nil, err
		}
		u.Scheme = bucketU.Scheme
		u.Host = bucketU.Host
		resultURL = u.String()
	}

	return &s3.PresignedPutResult{URL: resultURL}, nil
}

func (a *Aws) DeleteObject(ctx context.Context, name string) error {
	_, err := a.client.DeleteObject(ctx, &aws3.DeleteObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(name)})
	return err
}

func (a *Aws) CopyObject(ctx context.Context, src string, dst string) (*s3.CopyObjectInfo, error) {
	res, err := a.client.CopyObject(ctx, &aws3.CopyObjectInput{
		Bucket:     aws.String(a.bucket),
		CopySource: aws.String(a.bucket + "/" + src),
		Key:        aws.String(dst),
	})
	if err != nil {
		return nil, err
	}
	if res.CopyObjectResult == nil || res.CopyObjectResult.ETag == nil || *res.CopyObjectResult.ETag == "" {
		return nil, errors.New("CopyObject etag is nil")
	}
	return &s3.CopyObjectInfo{
		Key:  dst,
		ETag: a.formatETag(*res.CopyObjectResult.ETag),
	}, nil
}

func (a *Aws) StatObject(ctx context.Context, name string) (*s3.ObjectInfo, error) {
	res, err := a.client.HeadObject(ctx, &aws3.HeadObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(name)})
	if err != nil {
		return nil, err
	}
	if res.ETag == nil || *res.ETag == "" {
		return nil, errors.New("GetObjectAttributes etag is nil")
	}
	if res.ContentLength == nil {
		return nil, errors.New("GetObjectAttributes object size is nil")
	}
	info := &s3.ObjectInfo{
		ETag: a.formatETag(*res.ETag),
		Key:  name,
		Size: *res.ContentLength,
	}
	if res.LastModified == nil {
		info.LastModified = time.Unix(0, 0)
	} else {
		info.LastModified = *res.LastModified
	}
	return info, nil
}

func (a *Aws) InitiateMultipartUpload(ctx context.Context, name string, opt *s3.PutOption) (*s3.InitiateMultipartUploadResult, error) {
	res, err := a.client.CreateMultipartUpload(ctx, &aws3.CreateMultipartUploadInput{Bucket: aws.String(a.bucket), Key: aws.String(name)})
	if err != nil {
		return nil, err
	}
	if res.UploadId == nil || *res.UploadId == "" {
		return nil, errors.New("CreateMultipartUpload upload id is nil")
	}
	return &s3.InitiateMultipartUploadResult{
		Key:      name,
		Bucket:   name,
		UploadID: *res.UploadId,
	}, nil
}

func (a *Aws) CompleteMultipartUpload(ctx context.Context, uploadID string, name string, parts []s3.Part) (*s3.CompleteMultipartUploadResult, error) {
	params := &aws3.CompleteMultipartUploadInput{
		Bucket:   aws.String(a.bucket),
		Key:      aws.String(name),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: make([]types.CompletedPart, 0, len(parts)),
		},
	}
	for _, part := range parts {
		params.MultipartUpload.Parts = append(params.MultipartUpload.Parts, types.CompletedPart{
			ETag:       aws.String(part.ETag),
			PartNumber: aws.Int32(int32(part.PartNumber)),
		})
	}
	res, err := a.client.CompleteMultipartUpload(ctx, params)
	if err != nil {
		return nil, err
	}
	if res.ETag == nil || *res.ETag == "" {
		return nil, errors.New("CompleteMultipartUpload etag is nil")
	}
	info := &s3.CompleteMultipartUploadResult{
		Key:    name,
		Bucket: a.bucket,
		ETag:   a.formatETag(*res.ETag),
	}
	if res.Location != nil {
		info.Location = *res.Location
	}
	return info, nil
}

func (a *Aws) AbortMultipartUpload(ctx context.Context, uploadID string, name string) error {
	_, err := a.client.AbortMultipartUpload(ctx, &aws3.AbortMultipartUploadInput{Bucket: aws.String(a.bucket), Key: aws.String(name), UploadId: aws.String(uploadID)})
	return err
}

func (a *Aws) ListUploadedParts(ctx context.Context, uploadID string, name string, partNumberMarker int, maxParts int) (*s3.ListUploadedPartsResult, error) {
	params := &aws3.ListPartsInput{
		Bucket:           aws.String(a.bucket),
		Key:              aws.String(name),
		UploadId:         aws.String(uploadID),
		PartNumberMarker: aws.String(strconv.Itoa(partNumberMarker)),
		MaxParts:         aws.Int32(int32(maxParts)),
	}
	res, err := a.client.ListParts(ctx, params)
	if err != nil {
		return nil, err
	}
	info := &s3.ListUploadedPartsResult{
		Key:           name,
		UploadID:      uploadID,
		UploadedParts: make([]s3.UploadedPart, 0, len(res.Parts)),
	}
	if res.MaxParts != nil {
		info.MaxParts = int(*res.MaxParts)
	}
	if res.NextPartNumberMarker != nil {
		info.NextPartNumberMarker, _ = strconv.Atoi(*res.NextPartNumberMarker)
	}
	for _, part := range res.Parts {
		var val s3.UploadedPart
		if part.PartNumber != nil {
			val.PartNumber = int(*part.PartNumber)
		}
		if part.LastModified != nil {
			val.LastModified = *part.LastModified
		}
		if part.LastModified != nil {
			val.LastModified = *part.LastModified
		}
		if part.Size != nil {
			val.Size = *part.Size
		}
		info.UploadedParts = append(info.UploadedParts, val)
	}
	return info, nil
}

func (a *Aws) AuthSign(ctx context.Context, uploadID string, name string, expire time.Duration, partNumbers []int) (*s3.AuthSignResult, error) {
	res := &s3.AuthSignResult{
		Parts: make([]s3.SignPart, 0, len(partNumbers)),
	}
	params := &aws3.UploadPartInput{
		Bucket:   aws.String(a.bucket),
		Key:      aws.String(name),
		UploadId: aws.String(uploadID),
	}
	opt := aws3.WithPresignExpires(expire)
	for _, number := range partNumbers {
		params.PartNumber = aws.Int32(int32(number))
		val, err := a.presign.PresignUploadPart(ctx, params, opt)
		if err != nil {
			return nil, err
		}
		u, err := url.Parse(val.URL)
		if err != nil {
			return nil, err
		}

		// 如果配置了自定义的bucketURL，则替换URL的host部分
		if a.bucketURL != "" {
			bucketU, err := url.Parse(a.bucketURL)
			if err != nil {
				return nil, err
			}
			u.Scheme = bucketU.Scheme
			u.Host = bucketU.Host
		}

		query := u.Query()
		u.RawQuery = ""
		urlstr := u.String()
		if res.URL == "" {
			res.URL = urlstr
		}
		if res.URL == urlstr {
			urlstr = ""
		}
		res.Parts = append(res.Parts, s3.SignPart{
			PartNumber: number,
			URL:        urlstr,
			Query:      query,
			Header:     val.SignedHeader,
		})
	}
	return res, nil
}

func (a *Aws) AccessURL(ctx context.Context, name string, expire time.Duration, opt *s3.AccessURLOption) (string, error) {
	// 如果启用了公开读取模式且配置了bucketURL，则直接返回公开访问URL
	if a.publicRead && a.bucketURL != "" {
		bucketU, err := url.Parse(a.bucketURL)
		if err != nil {
			return "", err
		}

		// 构建直接访问URL：bucketURL + "/" + objectName
		baseURL := strings.TrimSuffix(bucketU.String(), "/")
		directURL := baseURL + "/" + name

		// 如果有选项参数，添加query参数
		if opt != nil {
			u, err := url.Parse(directURL)
			if err != nil {
				return "", err
			}
			query := u.Query()
			if opt.ContentType != "" {
				query.Set("response-content-type", opt.ContentType)
			}
			if opt.Filename != "" {
				query.Set("response-content-disposition", `attachment; filename*=UTF-8''`+url.PathEscape(opt.Filename))
			}
			u.RawQuery = query.Encode()
			return u.String(), nil
		}

		return directURL, nil
	}

	params := &aws3.GetObjectInput{
		Bucket: aws.String(a.bucket),
		Key:    aws.String(name),
	}
	res, err := a.presign.PresignGetObject(ctx, params, aws3.WithPresignExpires(expire), withDisableHTTPPresignerHeaderV4(opt))
	if err != nil {
		return "", err
	}

	// 如果配置了自定义的bucketURL（用于R2等服务的特殊访问模式），则替换URL的host部分
	if a.bucketURL != "" {
		u, err := url.Parse(res.URL)
		if err != nil {
			return "", err
		}
		bucketU, err := url.Parse(a.bucketURL)
		if err != nil {
			return "", err
		}
		u.Scheme = bucketU.Scheme
		u.Host = bucketU.Host
		return u.String(), nil
	}

	return res.URL, nil
}

func (a *Aws) FormData(ctx context.Context, name string, size int64, contentType string, duration time.Duration) (*s3.FormData, error) {
	return nil, errors.New("aws does not currently support form data file uploads")
}

func withDisableHTTPPresignerHeaderV4(opt *s3.AccessURLOption) func(options *aws3.PresignOptions) {
	return func(options *aws3.PresignOptions) {
		options.Presigner = &disableHTTPPresignerHeaderV4{
			opt:       opt,
			presigner: options.Presigner,
		}
	}
}

type disableHTTPPresignerHeaderV4 struct {
	opt       *s3.AccessURLOption
	presigner aws3.HTTPPresignerV4
}

func (d *disableHTTPPresignerHeaderV4) PresignHTTP(ctx context.Context, credentials aws.Credentials, r *http.Request, payloadHash string, service string, region string, signingTime time.Time, optFns ...func(*v4.SignerOptions)) (url string, signedHeader http.Header, err error) {
	optFns = append(optFns, func(options *v4.SignerOptions) {
		options.DisableHeaderHoisting = true
	})
	r.Header.Del("Amz-Sdk-Request")
	d.setOption(r.URL)
	return d.presigner.PresignHTTP(ctx, credentials, r, payloadHash, service, region, signingTime, optFns...)
}

func (d *disableHTTPPresignerHeaderV4) setOption(u *url.URL) {
	if d.opt == nil {
		return
	}
	query := u.Query()
	if d.opt.ContentType != "" {
		query.Set("response-content-type", d.opt.ContentType)
	}
	if d.opt.Filename != "" {
		query.Set("response-content-disposition", `attachment; filename*=UTF-8''`+url.PathEscape(d.opt.Filename))
	}
	u.RawQuery = query.Encode()
}

// customEndpointResolver 为R2等S3兼容服务提供自定义endpoint解析
type customEndpointResolver struct {
	endpoint string
}

func (r *customEndpointResolver) ResolveEndpoint(ctx context.Context, params aws3.EndpointParameters) (smithyendpoints.Endpoint, error) {
	if r.endpoint != "" {
		u, err := url.Parse(r.endpoint)
		if err != nil {
			return smithyendpoints.Endpoint{}, err
		}
		return smithyendpoints.Endpoint{
			URI: *u,
		}, nil
	}
	// 回退到默认resolver
	return aws3.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, params)
}
