package s3

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ton-storage-s3-cli/internal/database"
	"ton-storage-s3-cli/internal/ton"

	"github.com/johannesboyne/gofakes3"
)

var (
	emptyPrefix = &gofakes3.Prefix{}
)

type TonBackend struct {
	db		*database.DB
	ton		*ton.Service
	rootDir		string
	timeSource	gofakes3.TimeSource
}

var _ gofakes3.Backend = &TonBackend{}

func NewTonBackend(db *database.DB, tonSvc *ton.Service, rootDir string) *TonBackend {
	return &TonBackend{
		db:		db,
		ton:		tonSvc,
		rootDir:	rootDir,
		timeSource:	gofakes3.DefaultTimeSource(),
	}
}

func (b *TonBackend) ListBuckets() ([]gofakes3.BucketInfo, error) {
	buckets, err := b.db.ListBuckets(context.Background())
	if err != nil {
		return nil, err
	}

	var response []gofakes3.BucketInfo
	for _, bucket := range buckets {
		response = append(response, gofakes3.BucketInfo{
			Name:		bucket.Name,
			CreationDate:	gofakes3.NewContentTime(bucket.CreatedAt),
		})
	}
	return response, nil
}

func (b *TonBackend) ListBucket(name string, prefix *gofakes3.Prefix, page gofakes3.ListBucketPage) (*gofakes3.ObjectList, error) {
	if prefix == nil {
		prefix = emptyPrefix
	}
	exists, err := b.db.BucketExists(context.Background(), name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, gofakes3.BucketNotFound(name)
	}

	objects := gofakes3.NewObjectList()

	files, err := b.db.ListFiles(context.Background(), 2000, 0)
	if err != nil {
		return nil, err
	}

	var match gofakes3.PrefixMatch

	for _, f := range files {
		if f.BucketName != name {
			continue
		}

		if !prefix.Match(f.ObjectKey, &match) {
			continue
		} else if match.CommonPrefix {
			objects.AddPrefix(match.MatchedPart)
		} else {
			item := &gofakes3.Content{
				Key:		f.ObjectKey,
				LastModified:	gofakes3.NewContentTime(f.CreatedAt),
				ETag:		f.BagID,
				Size:		f.SizeBytes,
				StorageClass:	gofakes3.StorageStandard,
			}
			objects.Add(item)
		}
	}

	return objects, nil
}

func (b *TonBackend) CreateBucket(name string) error {
	exists, _ := b.db.BucketExists(context.Background(), name)
	if exists {
		return gofakes3.ResourceError(gofakes3.ErrBucketAlreadyExists, name)
	}
	return b.db.CreateBucket(context.Background(), name)
}

func (b *TonBackend) BucketExists(name string) (bool, error) {
	return b.db.BucketExists(context.Background(), name)
}

func (b *TonBackend) DeleteBucket(name string) error {
	return b.db.DeleteBucket(context.Background(), name)
}

func (b *TonBackend) ForceDeleteBucket(name string) error {
	return b.db.DeleteBucket(context.Background(), name)
}

func (b *TonBackend) HeadObject(bucketName, objectName string) (*gofakes3.Object, error) {
	fMeta, err := b.db.GetFileMeta(context.Background(), bucketName, objectName)
	if err != nil {
		return nil, gofakes3.KeyNotFound(objectName)
	}

	bagBytes, _ := hex.DecodeString(fMeta.BagID)

	return &gofakes3.Object{
		Name:		objectName,
		Size:		fMeta.SizeBytes,
		Hash:		bagBytes,
		Contents:	io.NopCloser(strings.NewReader("")),
		Metadata: map[string]string{
			"Last-Modified": fMeta.CreatedAt.Format(time.RFC1123),
		},
	}, nil
}

func (b *TonBackend) GetObject(bucketName, objectName string, rangeRequest *gofakes3.ObjectRangeRequest) (*gofakes3.Object, error) {
	fMeta, err := b.db.GetFileMeta(context.Background(), bucketName, objectName)
	if err != nil {
		return nil, gofakes3.KeyNotFound(objectName)
	}

	bagBytes, _ := hex.DecodeString(fMeta.BagID)

	localPath := filepath.Join(b.rootDir, objectName)
	downloadPath := filepath.Join(b.rootDir, fMeta.BagID, objectName)
	finalPath := ""

	if _, err := os.Stat(localPath); err == nil {
		finalPath = localPath
	} else if _, err := os.Stat(downloadPath); err == nil {
		finalPath = downloadPath
	} else {

		if err := b.ton.DownloadBag(context.Background(), bagBytes); err != nil {
			return nil, fmt.Errorf("TON download init failed: %v", err)
		}
		path, err := b.ton.WaitForFile(context.Background(), bagBytes, objectName)
		if err != nil {
			return nil, fmt.Errorf("timeout restoring file from TON: %v", err)
		}
		finalPath = path
	}

	f, err := os.Open(finalPath)
	if err != nil {
		return nil, gofakes3.ErrInternal
	}
	stat, _ := f.Stat()

	return &gofakes3.Object{
		Name:		objectName,
		Size:		stat.Size(),
		Hash:		bagBytes,
		Contents:	f,
		Metadata: map[string]string{
			"Last-Modified": fMeta.CreatedAt.Format(time.RFC1123),
		},
	}, nil
}

func (b *TonBackend) PutObject(
	bucketName, objectName string,
	meta map[string]string,
	input io.Reader,
	size int64,
	conditions *gofakes3.PutConditions,
) (result gofakes3.PutObjectResult, err error) {

	exists, err := b.db.BucketExists(context.Background(), bucketName)
	if err != nil {
		return result, err
	}
	if !exists {
		return result, gofakes3.BucketNotFound(bucketName)
	}

	localPath := filepath.Join(b.rootDir, objectName)
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return result, err
	}

	tmpFile, err := os.Create(localPath)
	if err != nil {
		return result, err
	}

	hasher := md5.New()
	writer := io.MultiWriter(tmpFile, hasher)

	if size == -1 {
		copied, err := io.Copy(writer, input)
		if err != nil {
			tmpFile.Close()
			return result, err
		}
		size = copied
	} else {
		if _, err := io.CopyN(writer, input, size); err != nil {
			tmpFile.Close()
			return result, err
		}
	}
	tmpFile.Close()

	pathForTon := strings.ReplaceAll(localPath, "\\", "/")
	bagIDBytes, err := b.ton.CreateBag(context.Background(), pathForTon)
	if err != nil {
		return result, fmt.Errorf("TON create bag failed: %w", err)
	}
	bagIDHex := hex.EncodeToString(bagIDBytes)

	targetReplicas := 1
	if val, ok := meta["replicas"]; ok {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			targetReplicas = n
		}
	}

	file := &database.File{
		BucketName:	bucketName,
		ObjectKey:	objectName,
		BagID:		bagIDHex,
		SizeBytes:	size,
		TargetReplicas:	targetReplicas,
		Status:		"pending",
	}

	if _, err := b.db.CreateFile(context.Background(), file); err != nil {
		return result, fmt.Errorf("DB error: %w", err)
	}

	return gofakes3.PutObjectResult{}, nil
}

func (b *TonBackend) CopyObject(srcBucket, srcKey, dstBucket, dstKey string, meta map[string]string) (result gofakes3.CopyObjectResult, err error) {
	return gofakes3.CopyObject(b, srcBucket, srcKey, dstBucket, dstKey, meta)
}

func (b *TonBackend) DeleteObject(bucketName, objectName string) (result gofakes3.ObjectDeleteResult, err error) {
	localPath := filepath.Join(b.rootDir, objectName)
	os.Remove(localPath)
	return result, nil
}

func (b *TonBackend) DeleteMulti(bucketName string, objects ...string) (result gofakes3.MultiDeleteResult, err error) {
	for _, obj := range objects {
		if _, err := b.DeleteObject(bucketName, obj); err != nil {
			result.Error = append(result.Error, gofakes3.ErrorResult{
				Key:	obj, Code: gofakes3.ErrInternal, Message: err.Error(),
			})
		} else {
			result.Deleted = append(result.Deleted, gofakes3.ObjectID{Key: obj})
		}
	}
	return result, nil
}
