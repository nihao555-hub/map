package s3uploader

import (
	"fmt"
	"os"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

func aliyunEndpoint(opt Options) string {
	ep := strings.TrimSpace(opt.Endpoint)
	if ep == "" && strings.HasPrefix(strings.TrimSpace(opt.Region), "oss-") {
		ep = strings.TrimSpace(opt.Region) + ".aliyuncs.com"
	}
	ep = strings.TrimPrefix(ep, "https://")
	ep = strings.TrimPrefix(ep, "http://")
	return strings.TrimRight(ep, "/")
}

func newAliyunClient(opt Options) (*oss.Client, error) {
	ep := aliyunEndpoint(opt)
	if !strings.Contains(ep, "://") {
		ep = "https://" + ep
	}
	return oss.New(ep, opt.AccessKey, opt.SecretKey, oss.Timeout(30, 600))
}

func uploadAliyunFile(opt Options, localPath string) error {
	client, err := newAliyunClient(opt)
	if err != nil {
		return err
	}
	bucket, err := client.Bucket(opt.Bucket)
	if err != nil {
		return err
	}
	return bucket.UploadFile(opt.Key, localPath, 16*1024*1024, oss.Routines(3))
}

func downloadAliyunFile(opt Options, destPath string) (int64, error) {
	client, err := newAliyunClient(opt)
	if err != nil {
		return 0, err
	}
	bucket, err := client.Bucket(opt.Bucket)
	if err != nil {
		return 0, err
	}
	if err := bucket.DownloadFile(opt.Key, destPath, 8*1024*1024, oss.Routines(3)); err != nil {
		return 0, err
	}
	st, err := os.Stat(destPath)
	if err != nil {
		return 0, err
	}
	if st.Size() < 1024 {
		return st.Size(), fmt.Errorf("下载只有 %d bytes，不像商户库", st.Size())
	}
	return st.Size(), nil
}
