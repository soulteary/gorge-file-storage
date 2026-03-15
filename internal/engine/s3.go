package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Engine struct {
	client       *s3.Client
	bucket       string
	instanceName string
}

type S3Config struct {
	Bucket       string
	AccessKey    string
	SecretKey    string
	Region       string
	Endpoint     string
	InstanceName string
}

func NewS3Engine(cfg S3Config) (*S3Engine, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("S3 bucket is required")
	}

	client := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		UsePathStyle: true,
	})

	return &S3Engine{
		client:       client,
		bucket:       cfg.Bucket,
		instanceName: cfg.InstanceName,
	}, nil
}

func (e *S3Engine) Identifier() string { return "amazon-s3" }
func (e *S3Engine) Priority() int      { return 100 }
func (e *S3Engine) CanWrite() bool     { return true }
func (e *S3Engine) HasSizeLimit() bool { return false }
func (e *S3Engine) MaxFileSize() int64 { return 0 }

func (e *S3Engine) WriteFile(ctx context.Context, data []byte, _ WriteParams) (string, error) {
	key, err := e.generateKey()
	if err != nil {
		return "", err
	}

	_, err = e.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(e.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return "", fmt.Errorf("s3 put: %w", err)
	}
	return key, nil
}

func (e *S3Engine) ReadFile(ctx context.Context, handle string) (io.ReadCloser, error) {
	out, err := e.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(e.bucket),
		Key:    aws.String(handle),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get: %w", err)
	}
	return out.Body, nil
}

func (e *S3Engine) DeleteFile(ctx context.Context, handle string) error {
	_, err := e.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(e.bucket),
		Key:    aws.String(handle),
	})
	if err != nil {
		return fmt.Errorf("s3 delete: %w", err)
	}
	return nil
}

// generateKey produces S3 keys compatible with the PHP engine:
// phabricator[/instance]/ab/cd/ef1234567890abcdef
func (e *S3Engine) generateKey() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random key: %w", err)
	}
	seed := hex.EncodeToString(b)

	parts := "phabricator"
	if e.instanceName != "" {
		parts += "/" + e.instanceName
	}
	parts += fmt.Sprintf("/%s/%s/%s", seed[:2], seed[2:4], seed[4:])
	return parts, nil
}
