package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// CloudStorage implements storage.Storage using signed URLs minted by the cloud.
type CloudStorage struct {
	c *Client
	// blobHTTP has no timeout, unlike c's client: blob bodies (hundreds of MB
	// of demo/artifact data) can take far longer than the 60s control-call
	// budget, so transfers rely on caller cancellation instead of a deadline.
	blobHTTP *http.Client
}

func NewCloudStorage(c *Client) *CloudStorage {
	return &CloudStorage{c: c, blobHTTP: &http.Client{}}
}

func (s *CloudStorage) Open(key string) (io.ReadCloser, error) {
	ctx := context.Background()
	var out struct {
		URL string `json:"url"`
	}
	if _, err := s.c.Do(ctx, "GET", "/api/agent/blobs/download?key="+url.QueryEscape(key), nil, &out); err != nil {
		return nil, err
	}
	resp, err := s.blobHTTP.Get(out.URL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: %d", key, resp.StatusCode)
	}
	return resp.Body, nil
}

func (s *CloudStorage) Put(key string, r io.Reader) error {
	ctx := context.Background()
	var out struct {
		URL string `json:"url"`
	}
	if _, err := s.c.Do(ctx, "POST", "/api/agent/blobs/sign-upload", map[string]string{"key": key}, &out); err != nil {
		return err
	}
	buf, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", out.URL, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := s.blobHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("upload %s: %d", key, resp.StatusCode)
	}
	return nil
}

func (s *CloudStorage) Exists(key string) (bool, error) {
	var out struct {
		Exists bool `json:"exists"`
	}
	if _, err := s.c.Do(context.Background(), "GET", "/api/agent/blobs/exists?key="+url.QueryEscape(key), nil, &out); err != nil {
		return false, err
	}
	return out.Exists, nil
}
