// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cont

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/openimsdk/tools/s3"

	"github.com/google/uuid"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
)

func New(cache S3Cache, impl s3.Interface, skipETagValidation ...bool) *Controller {
	skip := false // 默认值：不跳过ETag验证
	if len(skipETagValidation) > 0 {
		skip = skipETagValidation[0]
	}
	return &Controller{
		cache:              cache,
		impl:               impl,
		skipETagValidation: skip,
	}
}

type Controller struct {
	cache              S3Cache
	impl               s3.Interface
	skipETagValidation bool // 是否跳过ETag验证（用于Cloudflare R2等存储服务）
}

func (c *Controller) Engine() string {
	return c.impl.Engine()
}

func (c *Controller) HashPath(md5 string) string {
	return path.Join(hashPath, md5)
}

func (c *Controller) NowPath() string {
	now := time.Now()
	return path.Join(
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", now.Month()),
		fmt.Sprintf("%02d", now.Day()),
		fmt.Sprintf("%02d", now.Hour()),
		fmt.Sprintf("%02d", now.Minute()),
		fmt.Sprintf("%02d", now.Second()),
	)
}

func (c *Controller) UUID() string {
	id := uuid.New()
	return hex.EncodeToString(id[:])
}

func (c *Controller) PartSize(ctx context.Context, size int64) (int64, error) {
	return c.impl.PartSize(ctx, size)
}

func (c *Controller) PartLimit() (*s3.PartLimit, error) {
	return c.impl.PartLimit()
}

func (c *Controller) StatObject(ctx context.Context, name string) (*s3.ObjectInfo, error) {
	return c.cache.GetKey(ctx, c.impl.Engine(), name)
}

func (c *Controller) GetHashObject(ctx context.Context, hash string) (*s3.ObjectInfo, error) {
	return c.StatObject(ctx, c.HashPath(hash))
}

func (c *Controller) InitiateUpload(ctx context.Context, hash string, size int64, expire time.Duration, maxParts int) (*InitiateUploadResult, error) {
	//defer log.ZDebug(ctx, "return")
	//if size < 0 {
	//	return nil, errors.New("invalid size")
	//}
	//if hashBytes, err := hex.DecodeString(hash); err != nil {
	//	return nil, err
	//} else if len(hashBytes) != md5.Size {
	//	return nil, errors.New("invalid md5")
	//}
	//partSize, err := c.impl.PartSize(ctx, size)
	//if err != nil {
	//	return nil, err
	//}
	//partNumber := int(size / partSize)
	//if size%partSize > 0 {
	//	partNumber++
	//}
	//if maxParts > 0 && partNumber > 0 && partNumber < maxParts {
	//	return nil, fmt.Errorf("too many parts: %d", partNumber)
	//}
	//if info, err := c.StatObject(ctx, c.HashPath(hash)); err == nil {
	//	return nil, &HashAlreadyExistsError{Object: info}
	//} else if !c.impl.IsNotFound(err) {
	//	return nil, err
	//}
	//if size <= partSize {
	//	// Pre-signed upload
	//	key := path.Join(tempPath, c.NowPath(), fmt.Sprintf("%s_%d_%s.presigned", hash, size, c.UUID()))
	//	rawURL, err := c.impl.PresignedPutObject(ctx, key, expire)
	//	if err != nil {
	//		return nil, err
	//	}
	//	return &InitiateUploadResult{
	//		UploadID: newMultipartUploadID(multipartUploadID{
	//			Type: UploadTypePresigned,
	//			ID:   "",
	//			Key:  key,
	//			Size: size,
	//			Hash: hash,
	//		}),
	//		PartSize: partSize,
	//		Sign: &s3.AuthSignResult{
	//			Parts: []s3.SignPart{
	//				{
	//					PartNumber: 1,
	//					URL:        rawURL,
	//				},
	//			},
	//		},
	//	}, nil
	//} else {
	//	// Fragment upload
	//	upload, err := c.impl.InitiateMultipartUpload(ctx, c.HashPath(hash))
	//	if err != nil {
	//		return nil, err
	//	}
	//	if maxParts < 0 {
	//		maxParts = partNumber
	//	}
	//	var authSign *s3.AuthSignResult
	//	if maxParts > 0 {
	//		partNumbers := make([]int, maxParts)
	//		for i := 0; i < maxParts; i++ {
	//			partNumbers[i] = i + 1
	//		}
	//		authSign, err = c.impl.AuthSign(ctx, upload.UploadID, upload.Key, time.Hour*24, partNumbers)
	//		if err != nil {
	//			return nil, err
	//		}
	//	}
	//	return &InitiateUploadResult{
	//		UploadID: newMultipartUploadID(multipartUploadID{
	//			Type: UploadTypeMultipart,
	//			ID:   upload.UploadID,
	//			Key:  upload.Key,
	//			Size: size,
	//			Hash: hash,
	//		}),
	//		PartSize: partSize,
	//		Sign:     authSign,
	//	}, nil
	//}
	return c.InitiateUploadContentType(ctx, hash, size, expire, maxParts, "")
}

//func (c *Controller) presignedPutObject(ctx context.Context, name string, expire time.Duration, contentType string) (string, error) {
//	if contentType != "" {
//		if put, ok := c.impl.(s3.PresignedPutObjectWithContentType); ok {
//			return put.PresignedPutObjectContentType(ctx, name, expire, contentType)
//		}
//	}
//	return c.impl.PresignedPutObject(ctx, name, expire)
//}
//
//func (c *Controller) initiateMultipartUpload(ctx context.Context, name string, contentType string) (*s3.InitiateMultipartUploadResult, error) {
//	if contentType != "" {
//		if put, ok := c.impl.(s3.InitiateMultipartUploadWithContentType); ok {
//			return put.InitiateMultipartUploadContentType(ctx, name, contentType)
//		}
//	}
//	return c.impl.InitiateMultipartUpload(ctx, name)
//}

func (c *Controller) InitiateUploadContentType(ctx context.Context, hash string, size int64, expire time.Duration, maxParts int, contentType string) (*InitiateUploadResult, error) {
	defer log.ZDebug(ctx, "return")
	if size < 0 {
		return nil, errors.New("invalid size")
	}
	if hashBytes, err := hex.DecodeString(hash); err != nil {
		return nil, err
	} else if len(hashBytes) != md5.Size {
		return nil, errors.New("invalid md5")
	}
	partSize, err := c.impl.PartSize(ctx, size)
	if err != nil {
		return nil, err
	}
	partNumber := int(size / partSize)
	if size%partSize > 0 {
		partNumber++
	}
	if maxParts > 0 && partNumber > 0 && partNumber < maxParts {
		return nil, fmt.Errorf("too many parts: %d", partNumber)
	}
	if info, err := c.StatObject(ctx, c.HashPath(hash)); err == nil {
		return nil, &HashAlreadyExistsError{Object: info}
	} else if !c.impl.IsNotFound(err) {
		return nil, err
	}
	if size <= partSize {
		// Pre-signed upload
		key := path.Join(tempPath, c.NowPath(), fmt.Sprintf("%s_%d_%s.presigned", hash, size, c.UUID()))
		result, err := c.impl.PresignedPutObject(ctx, key, expire, &s3.PutOption{ContentType: contentType})
		if err != nil {
			return nil, err
		}
		return &InitiateUploadResult{
			UploadID: newMultipartUploadID(multipartUploadID{
				Type: UploadTypePresigned,
				ID:   "",
				Key:  key,
				Size: size,
				Hash: hash,
			}),
			PartSize: partSize,
			Sign: &s3.AuthSignResult{
				Parts: []s3.SignPart{
					{
						PartNumber: 1,
						URL:        result.URL,
						Header:     result.Header,
					},
				},
			},
		}, nil
	} else {
		// Fragment upload
		upload, err := c.impl.InitiateMultipartUpload(ctx, c.HashPath(hash), &s3.PutOption{ContentType: contentType})
		if err != nil {
			return nil, err
		}
		if maxParts < 0 {
			maxParts = partNumber
		}
		var authSign *s3.AuthSignResult
		if maxParts > 0 {
			partNumbers := make([]int, maxParts)
			for i := 0; i < maxParts; i++ {
				partNumbers[i] = i + 1
			}
			authSign, err = c.impl.AuthSign(ctx, upload.UploadID, upload.Key, time.Hour*24, partNumbers)
			if err != nil {
				return nil, err
			}
		}
		return &InitiateUploadResult{
			UploadID: newMultipartUploadID(multipartUploadID{
				Type: UploadTypeMultipart,
				ID:   upload.UploadID,
				Key:  upload.Key,
				Size: size,
				Hash: hash,
			}),
			PartSize: partSize,
			Sign:     authSign,
		}, nil
	}
}

func (c *Controller) CompleteUpload(ctx context.Context, uploadID string, partHashs []string) (*UploadResult, error) {
	defer log.ZDebug(ctx, "return")
	upload, err := parseMultipartUploadID(uploadID)
	if err != nil {
		return nil, err
	}
	if md5Sum := md5.Sum([]byte(strings.Join(partHashs, partSeparator))); hex.EncodeToString(md5Sum[:]) != upload.Hash {
		return nil, errors.New("md5 mismatching")
	}
	if info, err := c.StatObject(ctx, c.HashPath(upload.Hash)); err == nil {
		return &UploadResult{
			Key:  info.Key,
			Size: info.Size,
			Hash: info.ETag,
		}, nil
	} else if !c.IsNotFound(err) {
		return nil, err
	}
	cleanObject := make(map[string]struct{})
	defer func() {
		for key := range cleanObject {
			_ = c.impl.DeleteObject(ctx, key)
		}
	}()
	var targetKey string
	switch upload.Type {
	case UploadTypeMultipart:
		parts := make([]s3.Part, len(partHashs))
		for i, part := range partHashs {
			parts[i] = s3.Part{
				PartNumber: i + 1,
				ETag:       part,
			}
		}
		// todo: Validation size
		result, err := c.impl.CompleteMultipartUpload(ctx, upload.ID, upload.Key, parts)
		if err != nil {
			return nil, err
		}
		targetKey = result.Key
	case UploadTypePresigned:
		uploadInfo, err := c.StatObject(ctx, upload.Key)
		if err != nil {
			return nil, err
		}
		cleanObject[uploadInfo.Key] = struct{}{}
		if uploadInfo.Size != upload.Size {
			return nil, errors.New("upload size mismatching")
		}
		md5Sum := md5.Sum([]byte(strings.Join([]string{uploadInfo.ETag}, partSeparator)))
		if md5val := hex.EncodeToString(md5Sum[:]); md5val != upload.Hash {
			return nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("md5 mismatching %s != %s", md5val, upload.Hash))
		}
		// Prevents concurrent operations at this time that cause files to be overwritten
		copyInfo, err := c.impl.CopyObject(ctx, uploadInfo.Key, upload.Key+"."+c.UUID())
		if err != nil {
			return nil, err
		}
		cleanObject[copyInfo.Key] = struct{}{}
		if copyInfo.ETag != uploadInfo.ETag {
			if c.skipETagValidation {
				log.ZWarn(ctx, "[s3] ETag mismatch detected, but skipETagValidation is enabled",
					nil,
					"engine", c.impl.Engine(),
					"originalETag", uploadInfo.ETag,
					"copyETag", copyInfo.ETag,
					"key", uploadInfo.Key)
			} else {
				return nil, errors.New("[concurrency]copy md5 mismatching")
			}
		}
		hashCopyInfo, err := c.impl.CopyObject(ctx, copyInfo.Key, c.HashPath(upload.Hash))
		if err != nil {
			return nil, err
		}
		log.ZInfo(ctx, "hashCopyInfo", "value", fmt.Sprintf("%+v", hashCopyInfo))
		targetKey = hashCopyInfo.Key
	default:
		return nil, errors.New("invalid upload id type")
	}
	if err := c.cache.DelS3Key(ctx, c.impl.Engine(), targetKey); err != nil {
		return nil, err
	}
	return &UploadResult{
		Key:  targetKey,
		Size: upload.Size,
		Hash: upload.Hash,
	}, nil
}

func (c *Controller) AuthSign(ctx context.Context, uploadID string, partNumbers []int) (*s3.AuthSignResult, error) {
	upload, err := parseMultipartUploadID(uploadID)
	if err != nil {
		return nil, err
	}
	switch upload.Type {
	case UploadTypeMultipart:
		return c.impl.AuthSign(ctx, upload.ID, upload.Key, time.Hour*24, partNumbers)
	case UploadTypePresigned:
		return nil, errors.New("presigned id not support auth sign")
	default:
		return nil, errors.New("invalid upload id type")
	}
}

func (c *Controller) IsNotFound(err error) bool {
	return c.impl.IsNotFound(err) || errs.ErrRecordNotFound.Is(err)
}

func (c *Controller) AccessURL(ctx context.Context, name string, expire time.Duration, opt *s3.AccessURLOption) (string, error) {
	//if opt.Image != nil {
	//	opt.Filename = ""
	//	opt.ContentType = ""
	//}
	return c.impl.AccessURL(ctx, name, expire, opt)
}

func (c *Controller) FormData(ctx context.Context, name string, size int64, contentType string, duration time.Duration) (*s3.FormData, error) {
	return c.impl.FormData(ctx, name, size, contentType, duration)
}

func (c *Controller) DeleteObject(ctx context.Context, name string) error {
	return c.impl.DeleteObject(ctx, name)
}
