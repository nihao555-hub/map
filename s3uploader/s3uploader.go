package s3uploader

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Uploader struct {
	client *s3.Client
}

// Options is S3 or S3-compatible object storage (Aliyun OSS, MinIO, R2).
type Options struct {
	AccessKey string
	SecretKey string
	Region    string
	Endpoint  string
	Bucket    string
	Key       string
	PathStyle bool
}

func New(accessKey, secretKey, region string) *Uploader {
	return NewWithOptions(Options{AccessKey: accessKey, SecretKey: secretKey, Region: region})
}

func NewWithOptions(opt Options) *Uploader {
	opt.AccessKey = strings.TrimSpace(opt.AccessKey)
	opt.SecretKey = strings.TrimSpace(opt.SecretKey)
	opt.Region = strings.TrimSpace(opt.Region)
	opt.Endpoint = strings.TrimSpace(opt.Endpoint)
	if opt.AccessKey == "" || opt.SecretKey == "" {
		return nil
	}
	if opt.Region == "" {
		opt.Region = "us-east-1"
	}
	creds := credentials.NewStaticCredentialsProvider(opt.AccessKey, opt.SecretKey, "")
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithCredentialsProvider(creds),
		config.WithRegion(opt.Region),
	)
	if err != nil {
		return nil
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if opt.Endpoint == "" {
			return
		}
		o.BaseEndpoint = aws.String(normalizeEndpoint(opt.Endpoint))
		o.UsePathStyle = opt.PathStyle
	})
	return &Uploader{client: client}
}

// OptionsFromEnv reads Aliyun OSS / AWS S3 settings. Secrets stay in the
// environment; they are never written to the merchant DB or git.
func OptionsFromEnv() Options {
	pathStyle := true
	switch strings.ToLower(strings.TrimSpace(firstEnv("ENGINE_OSS_PATH_STYLE", "OSS_PATH_STYLE"))) {
	case "0", "false", "no", "virtual":
		pathStyle = false
	}
	return Options{
		AccessKey: firstEnv("ENGINE_OSS_ACCESS_KEY", "OSS_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID"),
		SecretKey: firstEnv("ENGINE_OSS_SECRET_KEY", "OSS_ACCESS_KEY_SECRET", "AWS_SECRET_ACCESS_KEY"),
		Region:    firstEnv("ENGINE_OSS_REGION", "OSS_REGION", "AWS_REGION"),
		Endpoint:  firstEnv("ENGINE_OSS_ENDPOINT", "OSS_ENDPOINT"),
		Bucket:    firstEnv("ENGINE_OSS_BUCKET", "OSS_BUCKET"),
		Key:       firstNonEmpty(firstEnv("ENGINE_OSS_KEY", "OSS_KEY"), "engine/merchants.db"),
		PathStyle: pathStyle,
	}
}

func (o Options) Ready() bool {
	return strings.TrimSpace(o.AccessKey) != "" && strings.TrimSpace(o.SecretKey) != "" && strings.TrimSpace(o.Bucket) != ""
}

func normalizeEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		return strings.TrimRight(raw, "/")
	}
	return "https://" + strings.TrimRight(raw, "/")
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (u *Uploader) Upload(ctx context.Context, bucketName, key string, body io.Reader) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   body,
	}

	_, err := u.client.PutObject(ctx, input)
	if err != nil {
		return err
	}

	return nil
}

func (u *Uploader) Download(ctx context.Context, bucketName, key string, dest io.Writer) (int64, error) {
	if u == nil || u.client == nil {
		return 0, io.ErrClosedPipe
	}
	out, err := u.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, err
	}
	defer out.Body.Close()
	return io.Copy(dest, out.Body)
}
