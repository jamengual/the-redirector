// Package providers contains configuration source implementations.
// This file implements Azure Blob Storage integration.
package providers

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/rs/zerolog/log"

	"github.com/jamengual/the-redirector/internal/config"
)

func init() {
	Registry.Register("azureblob", NewAzureBlobSourceFromMap)
}

// AzureBlobSourceConfig configures the Azure Blob source.
type AzureBlobSourceConfig struct {
	// StorageAccount is the Azure storage account name
	StorageAccount string `yaml:"storage_account" json:"storage_account"`

	// Container is the blob container name
	Container string `yaml:"container" json:"container"`

	// BlobName is the name of the blob (file) in the container
	BlobName string `yaml:"blob_name" json:"blob_name"`

	// ConnectionString for authentication (optional, alternative to account/key)
	// Format: DefaultEndpointsProtocol=https;AccountName=...;AccountKey=...
	ConnectionString string `yaml:"connection_string" json:"connection_string"`

	// AccountKey for Shared Key authentication (optional)
	AccountKey string `yaml:"account_key" json:"account_key"`

	// SASToken for SAS authentication (optional)
	// Should not include the leading '?'
	SASToken string `yaml:"sas_token" json:"sas_token"`

	// UseDefaultCredential uses Azure Default Credential (managed identity, CLI, etc.)
	// This is the recommended authentication method for production
	UseDefaultCredential bool `yaml:"use_default_credential" json:"use_default_credential"`

	// PollInterval for checking changes (default: 5m)
	PollInterval time.Duration `yaml:"poll_interval" json:"poll_interval"`
}

// AzureBlobSource fetches configuration from Azure Blob Storage.
type AzureBlobSource struct {
	cfg      AzureBlobSourceConfig
	client   *azblob.Client
	lastETag string
	mu       sync.RWMutex
	stopCh   chan struct{}
	options  SourceOptions
}

// NewAzureBlobSourceFromMap creates a new Azure Blob source from a config map.
func NewAzureBlobSourceFromMap(cfg map[string]interface{}) (Source, error) {
	storageAccount, _ := cfg["storage_account"].(string)
	container, ok := cfg["container"].(string)
	if !ok || container == "" {
		return nil, fmt.Errorf("azureblob source requires 'container'")
	}

	blobName, ok := cfg["blob_name"].(string)
	if !ok || blobName == "" {
		return nil, fmt.Errorf("azureblob source requires 'blob_name'")
	}

	sourceCfg := AzureBlobSourceConfig{
		StorageAccount: storageAccount,
		Container:      container,
		BlobName:       blobName,
		PollInterval:   5 * time.Minute,
	}

	if connStr, ok := cfg["connection_string"].(string); ok {
		sourceCfg.ConnectionString = connStr
	}

	if accountKey, ok := cfg["account_key"].(string); ok {
		sourceCfg.AccountKey = accountKey
	}

	if sasToken, ok := cfg["sas_token"].(string); ok {
		sourceCfg.SASToken = sasToken
	}

	if useDefault, ok := cfg["use_default_credential"].(bool); ok {
		sourceCfg.UseDefaultCredential = useDefault
	}

	if interval, ok := cfg["poll_interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			sourceCfg.PollInterval = d
		}
	}

	return NewAzureBlobSource(context.Background(), sourceCfg)
}

// NewAzureBlobSource creates a new Azure Blob Storage configuration source.
func NewAzureBlobSource(ctx context.Context, cfg AzureBlobSourceConfig) (*AzureBlobSource, error) {
	if cfg.Container == "" {
		return nil, fmt.Errorf("container is required")
	}
	if cfg.BlobName == "" {
		return nil, fmt.Errorf("blob_name is required")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Minute
	}

	var client *azblob.Client
	var err error

	// Authentication priority:
	// 1. Connection string
	// 2. Account + SAS token
	// 3. Account + Account key
	// 4. Default credential (managed identity, CLI, etc.)

	switch {
	case cfg.ConnectionString != "":
		client, err = azblob.NewClientFromConnectionString(cfg.ConnectionString, nil)
		if err != nil {
			return nil, fmt.Errorf("creating client from connection string: %w", err)
		}

	case cfg.SASToken != "":
		if cfg.StorageAccount == "" {
			return nil, fmt.Errorf("storage_account is required when using sas_token")
		}
		serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/?%s", cfg.StorageAccount, cfg.SASToken)
		client, err = azblob.NewClientWithNoCredential(serviceURL, nil)
		if err != nil {
			return nil, fmt.Errorf("creating client with SAS token: %w", err)
		}

	case cfg.AccountKey != "":
		if cfg.StorageAccount == "" {
			return nil, fmt.Errorf("storage_account is required when using account_key")
		}
		cred, err := azblob.NewSharedKeyCredential(cfg.StorageAccount, cfg.AccountKey)
		if err != nil {
			return nil, fmt.Errorf("creating shared key credential: %w", err)
		}
		serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", cfg.StorageAccount)
		client, err = azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("creating client with shared key: %w", err)
		}

	case cfg.UseDefaultCredential:
		if cfg.StorageAccount == "" {
			return nil, fmt.Errorf("storage_account is required when using default credential")
		}
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("creating default credential: %w", err)
		}
		serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", cfg.StorageAccount)
		client, err = azblob.NewClient(serviceURL, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("creating client with default credential: %w", err)
		}

	default:
		return nil, fmt.Errorf("no authentication method provided: use connection_string, account_key, sas_token, or use_default_credential")
	}

	return &AzureBlobSource{
		cfg:     cfg,
		client:  client,
		stopCh:  make(chan struct{}),
		options: DefaultSourceOptions(),
	}, nil
}

// Name returns the source name.
func (a *AzureBlobSource) Name() string {
	return "azureblob"
}

// Fetch retrieves the configuration from Azure Blob Storage.
func (a *AzureBlobSource) Fetch(ctx context.Context) (*config.Config, error) {
	data, etag, err := a.download(ctx)
	if err != nil {
		return nil, err
	}

	cfg, err := config.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Store ETag for change detection
	a.mu.Lock()
	a.lastETag = etag
	a.mu.Unlock()

	return cfg, nil
}

// download fetches the blob content.
func (a *AzureBlobSource) download(ctx context.Context) ([]byte, string, error) {
	resp, err := a.client.DownloadStream(ctx, a.cfg.Container, a.cfg.BlobName, nil)
	if err != nil {
		return nil, "", fmt.Errorf("downloading blob: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading blob: %w", err)
	}

	etag := ""
	if resp.ETag != nil {
		etag = string(*resp.ETag)
	}

	log.Debug().
		Str("container", a.cfg.Container).
		Str("blob", a.cfg.BlobName).
		Str("etag", etag).
		Int("bytes", len(data)).
		Msg("Downloaded config from Azure Blob")

	return data, etag, nil
}

// Watch polls Azure Blob Storage for changes and returns configs on a channel.
func (a *AzureBlobSource) Watch(ctx context.Context) (<-chan *config.Config, error) {
	ch := make(chan *config.Config, 1)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(a.cfg.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-a.stopCh:
				return
			case <-ticker.C:
				changed, err := a.hasChanged(ctx)
				if err != nil {
					log.Error().Err(err).Msg("Error checking Azure Blob for changes")
					continue
				}

				if !changed {
					continue
				}

				cfg, err := a.Fetch(ctx)
				if err != nil {
					log.Error().Err(err).Msg("Error fetching config from Azure Blob")
					continue
				}

				select {
				case ch <- cfg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}

// hasChanged checks if the blob has changed since last fetch.
func (a *AzureBlobSource) hasChanged(ctx context.Context) (bool, error) {
	a.mu.RLock()
	lastETag := a.lastETag
	a.mu.RUnlock()

	if lastETag == "" {
		return true, nil // Never fetched
	}

	// Get blob properties without downloading content
	resp, err := a.client.ServiceClient().NewContainerClient(a.cfg.Container).NewBlobClient(a.cfg.BlobName).GetProperties(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("getting blob properties: %w", err)
	}

	currentETag := ""
	if resp.ETag != nil {
		currentETag = string(*resp.ETag)
	}

	changed := currentETag != lastETag

	if changed {
		log.Debug().
			Str("container", a.cfg.Container).
			Str("blob", a.cfg.BlobName).
			Str("old_etag", lastETag).
			Str("new_etag", currentETag).
			Msg("Azure Blob config changed")
	}

	return changed, nil
}

// SupportsWatch returns true - Azure Blob uses polling.
func (a *AzureBlobSource) SupportsWatch() bool {
	return true
}

// Validate checks that the blob is accessible.
func (a *AzureBlobSource) Validate(ctx context.Context) error {
	_, err := a.client.ServiceClient().NewContainerClient(a.cfg.Container).NewBlobClient(a.cfg.BlobName).GetProperties(ctx, nil)
	if err != nil {
		return fmt.Errorf("validating Azure Blob access: %w", err)
	}
	return nil
}

// Close releases any resources.
func (a *AzureBlobSource) Close() error {
	close(a.stopCh)
	return nil
}
