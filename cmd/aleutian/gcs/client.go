// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package gcs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

type Client struct {
	storageClient *storage.Client
	ProjectId     string
	BucketName    string
}

func NewClient(ctx context.Context, projectId, bucketName, saKeyPath string) (*Client, error) {
	if _, err := os.Stat(saKeyPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("service account key not found at path: %s. Please ensure you have the correct key and it is accessible", saKeyPath)
	}

	storageClient, err := storage.NewClient(ctx, option.WithCredentialsFile(saKeyPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS storage client: %w", err)
	}

	return &Client{
		storageClient: storageClient,
		ProjectId:     projectId,
		BucketName:    bucketName,
	}, nil
}

func (c *Client) UploadFile(ctx context.Context, localPath, gcsPath string) error {
	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open the local file: %s: %w", localPath, err)
	}
	defer localFile.Close()

	// Get a writer for the GCS object
	obj := c.storageClient.Bucket(c.BucketName).Object(gcsPath)
	writer := obj.NewWriter(ctx)
	writer.ContentType = "application/octet-stream"
	writer.CacheControl = "no-cache, no-store, must-revalidate"

	if _, err := io.Copy(writer, localFile); err != nil {
		return fmt.Errorf("failed to copy local file %s to GCS object %s: %w", localPath, gcsPath, err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close GCS writer for %s: %w", gcsPath, err)
	}
	fmt.Printf("Successfully uploaded %s to gs://%s/%s\n", localPath, c.BucketName, gcsPath)
	return nil
}

func (c *Client) UploadDir(ctx context.Context, localDir, gcsPrefix string) error {
	return filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			gcsPath := filepath.Join(gcsPrefix, info.Name())
			return c.UploadFile(ctx, path, gcsPath)
		}
		return nil
	})
}
