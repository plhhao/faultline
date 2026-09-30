package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type result struct {
	Operation  string `json:"operation"`
	Key        string `json:"key"`
	ElapsedMS  int64  `json:"elapsed_ms"`
	Bytes      int    `json:"bytes,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Exists     *bool  `json:"exists,omitempty"`
	Error      string `json:"error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

func main() {
	op := flag.String("op", "put", "put, get, head or delete")
	key := flag.String("key", "", "object key under S3_TEST_PREFIX")
	size := flag.Int("size", 65536, "deterministic PUT payload size")
	limit := flag.Duration("timeout", 10*time.Second, "operation deadline")
	flag.Parse()
	if *key == "" || *size < 0 || *size > 32<<20 || *limit <= 0 {
		fmt.Fprintln(os.Stderr, "invalid key, size or timeout")
		os.Exit(2)
	}
	for _, name := range []string{"S3_REGION", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "S3_BUCKET_NAME"} {
		if os.Getenv(name) == "" {
			fmt.Fprintf(os.Stderr, "missing %s\n", name)
			os.Exit(2)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *limit)
	defer cancel()
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(os.Getenv("S3_REGION")),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(os.Getenv("S3_ACCESS_KEY_ID"), os.Getenv("S3_SECRET_ACCESS_KEY"), "")),
		awsconfig.WithRetryMaxAttempts(1),
		awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		awsconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "SDK configuration failed")
		os.Exit(2)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint := os.Getenv("S3_ENDPOINT"); endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	bucket := aws.String(os.Getenv("S3_BUCKET_NAME"))
	start := time.Now()
	r := result{Operation: *op, Key: *key}
	switch *op {
	case "put":
		payload := make([]byte, *size)
		for i := range payload {
			payload[i] = byte((i*31 + 17) % 251)
		}
		r.Bytes = len(payload)
		hash := sha256.Sum256(payload)
		r.SHA256 = hex.EncodeToString(hash[:])
		_, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: bucket, Key: key, Body: bytes.NewReader(payload)})
	case "get":
		var out *s3.GetObjectOutput
		out, err = client.GetObject(ctx, &s3.GetObjectInput{Bucket: bucket, Key: key})
		if err == nil {
			defer out.Body.Close()
			var body []byte
			body, err = io.ReadAll(io.LimitReader(out.Body, 33<<20))
			r.Bytes = len(body)
			hash := sha256.Sum256(body)
			r.SHA256 = hex.EncodeToString(hash[:])
		}
	case "head":
		_, err = client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: bucket, Key: key})
		if err == nil {
			exists := true
			r.Exists = &exists
		}
	case "delete":
		_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: bucket, Key: key})
	default:
		fmt.Fprintln(os.Stderr, "invalid op")
		os.Exit(2)
	}
	r.ElapsedMS = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = fmt.Sprintf("%T: %v", err, err)
		var apiError smithy.APIError
		if errors.As(err, &apiError) {
			r.ErrorCode = apiError.ErrorCode()
		}
		var responseError *awshttp.ResponseError
		if errors.As(err, &responseError) {
			r.HTTPStatus = responseError.HTTPStatusCode()
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(r)
	if err != nil {
		os.Exit(1)
	}
}
