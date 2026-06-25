//go:build ignore

package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Config struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
}

func main() {
	log.Println("[Sim-CRR] Starting Cross-Region Replication simulator...")

	cfgEast := S3Config{
		Endpoint:  "http://127.0.0.1:9000",
		Bucket:    "transcoder-us-east",
		Region:    "us-east",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
	}

	cfgWest := S3Config{
		Endpoint:  "http://127.0.0.1:9010",
		Bucket:    "transcoder-eu-west",
		Region:    "eu-west",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
	}

	clientEast, err := createS3Client(cfgEast)
	if err != nil {
		log.Fatalf("Failed to create US-East S3 client: %v", err)
	}

	clientWest, err := createS3Client(cfgWest)
	if err != nil {
		log.Fatalf("Failed to create EU-West S3 client: %v", err)
	}

	ctx := context.Background()

	// Replication polling loop
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Sync US-East -> EU-West
		replicateDirection(ctx, clientEast, cfgEast.Bucket, clientWest, cfgWest.Bucket)
		// Sync EU-West -> US-East
		replicateDirection(ctx, clientWest, cfgWest.Bucket, clientEast, cfgEast.Bucket)
	}
}

func createS3Client(cfg S3Config) (*s3.Client, error) {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:           cfg.Endpoint,
			SigningRegion: cfg.Region,
		}, nil
	})

	credProvider := credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")

	awsCfg, err := awsconfig.LoadDefaultConfig(context.TODO(),
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithEndpointResolverWithOptions(customResolver),
		awsconfig.WithCredentialsProvider(credProvider),
	)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	return client, nil
}

func replicateDirection(ctx context.Context, srcClient *s3.Client, srcBucket string, dstClient *s3.Client, dstBucket string) {
	// List objects in source bucket
	listOut, err := srcClient.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(srcBucket),
	})
	if err != nil {
		return
	}

	for _, obj := range listOut.Contents {
		key := *obj.Key

		// Filter rules: Replicate ONLY manifest metadata files
		// (master.m3u8, manifest.mpd, job_completed.json, job_manifest.json)
		// DO NOT replicate raw source video segments or chunk segments (data gravity)
		isManifest := strings.HasSuffix(key, "master.m3u8") ||
			strings.HasSuffix(key, "manifest.mpd") ||
			strings.HasSuffix(key, "job_completed.json") ||
			strings.HasSuffix(key, "job_manifest.json")

		if !isManifest {
			continue
		}

		// Check if it already exists in the destination bucket and compare metadata
		needReplicate := true
		dstMeta, err := dstClient.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(dstBucket),
			Key:    aws.String(key),
		})
		if err == nil {
			srcSize := int64(0)
			if obj.Size != nil {
				srcSize = *obj.Size
			}
			dstSize := int64(0)
			if dstMeta.ContentLength != nil {
				dstSize = *dstMeta.ContentLength
			}

			srcETag := ""
			if obj.ETag != nil {
				srcETag = strings.Trim(*obj.ETag, "\"")
			}
			dstETag := ""
			if dstMeta.ETag != nil {
				dstETag = strings.Trim(*dstMeta.ETag, "\"")
			}

			if srcSize == dstSize && srcETag == dstETag && srcETag != "" {
				needReplicate = false
			}
		}

		if !needReplicate {
			continue
		}

		// Object missing in destination, replicate it!
		log.Printf("[Sim-CRR] Replicating manifest %s from bucket %s to %s", key, srcBucket, dstBucket)

		getOut, err := srcClient.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(srcBucket),
			Key:    aws.String(key),
		})
		if err != nil {
			log.Printf("[Sim-CRR] Error getting object %s: %v", key, err)
			continue
		}

		data, err := io.ReadAll(getOut.Body)
		getOut.Body.Close()
		if err != nil {
			log.Printf("[Sim-CRR] Error reading object body %s: %v", key, err)
			continue
		}

		_, err = dstClient.PutObject(ctx, &s3.PutObjectInput{
			Bucket:        aws.String(dstBucket),
			Key:           aws.String(key),
			Body:          bytes.NewReader(data),
			ContentLength: aws.Int64(int64(len(data))),
		})

		if err != nil {
			log.Printf("[Sim-CRR] Error replicating object %s: %v", key, err)
		} else {
			log.Printf("[Sim-CRR] Successfully replicated %s", key)
		}
	}
}
