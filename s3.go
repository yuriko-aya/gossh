package main

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func newS3PresignClient() (*s3.PresignClient, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.TODO(),
		awsconfig.WithRegion(config.S3.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			config.S3.AccessKeyID,
			config.S3.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	client := s3.NewFromConfig(cfg)
	return s3.NewPresignClient(client), nil
}

func presignPutURL(key string, expiry time.Duration) (string, error) {
	client, err := newS3PresignClient()
	if err != nil {
		return "", err
	}
	req, err := client.PresignPutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(config.S3.Bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned PUT URL: %w", err)
	}
	return req.URL, nil
}

func presignGetURL(key string, expiry time.Duration) (string, error) {
	client, err := newS3PresignClient()
	if err != nil {
		return "", err
	}
	req, err := client.PresignGetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(config.S3.Bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned GET URL: %w", err)
	}
	return req.URL, nil
}
