package s3uploader

import (
	"testing"
)

func TestNormalizeEndpoint(t *testing.T) {
	if got := normalizeEndpoint("oss-cn-hangzhou.aliyuncs.com"); got != "https://oss-cn-hangzhou.aliyuncs.com" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeEndpoint("https://oss-cn-hongkong.aliyuncs.com/"); got != "https://oss-cn-hongkong.aliyuncs.com" {
		t.Fatalf("got %q", got)
	}
}

func TestOptionsFromEnv(t *testing.T) {
	t.Setenv("ENGINE_OSS_ACCESS_KEY", "ak")
	t.Setenv("ENGINE_OSS_SECRET_KEY", "sk")
	t.Setenv("ENGINE_OSS_BUCKET", "gmaps-engine")
	t.Setenv("ENGINE_OSS_ENDPOINT", "oss-cn-hangzhou.aliyuncs.com")
	t.Setenv("ENGINE_OSS_KEY", "")
	t.Setenv("OSS_ACCESS_KEY_ID", "")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "")
	t.Setenv("OSS_BUCKET", "")
	t.Setenv("OSS_ENDPOINT", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	opt := OptionsFromEnv()
	if !opt.Ready() || opt.Key != "engine/merchants.db" || opt.Endpoint != "oss-cn-hangzhou.aliyuncs.com" {
		t.Fatalf("%+v", opt)
	}
	if NewWithOptions(opt) == nil {
		t.Fatal("client")
	}
}
