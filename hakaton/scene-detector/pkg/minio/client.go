package minio

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Client struct {
	minioClient *minio.Client
	bucketName  string
	endpoint    string
	useSSL      bool
}

func NewClient(endpoint, accessKey, secretKey, bucketName string, useSSL bool) (*Client, error) {
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	client := &Client{
		minioClient: minioClient,
		bucketName:  bucketName,
		endpoint:    endpoint,
		useSSL:      useSSL,
	}

	// Создаем bucket если не существует
	ctx := context.Background()
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket: %w", err)
	}

	if !exists {
		err = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	return client, nil
}

func (c *Client) UploadFile(ctx context.Context, reader io.Reader, size int64, contentType, objectName string) (string, error) {
	_, err := c.minioClient.PutObject(ctx, c.bucketName, objectName, reader, size,
		minio.PutObjectOptions{
			ContentType: contentType,
		})
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	// Генерируем presigned URL (действителен 7 дней)
	expiry := 7 * 24 * time.Hour

	// Исправление: используем nil вместо map[string]string
	// или можно создать url.Values если нужны параметры
	presignedURL, err := c.minioClient.PresignedGetObject(ctx, c.bucketName, objectName, expiry, nil)
	if err != nil {
		// Если не удалось получить presigned URL, возвращаем прямой URL
		scheme := "http"
		if c.useSSL {
			scheme = "https"
		}
		return fmt.Sprintf("%s://%s/%s/%s", scheme, c.endpoint, c.bucketName, objectName), nil
	}

	return presignedURL.String(), nil
}

// Альтернативная версия с дополнительными параметрами запроса
func (c *Client) UploadFileWithParams(ctx context.Context, reader io.Reader, size int64, contentType, objectName string, params map[string]string) (string, error) {
	_, err := c.minioClient.PutObject(ctx, c.bucketName, objectName, reader, size,
		minio.PutObjectOptions{
			ContentType: contentType,
		})
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	// Конвертируем map[string]string в url.Values
	reqParams := url.Values{}
	for key, value := range params {
		reqParams.Set(key, value)
	}

	expiry := 7 * 24 * time.Hour
	presignedURL, err := c.minioClient.PresignedGetObject(ctx, c.bucketName, objectName, expiry, reqParams)
	if err != nil {
		scheme := "http"
		if c.useSSL {
			scheme = "https"
		}
		return fmt.Sprintf("%s://%s/%s/%s", scheme, c.endpoint, c.bucketName, objectName), nil
	}

	return presignedURL.String(), nil
}
