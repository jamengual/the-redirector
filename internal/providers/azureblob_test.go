package providers

import (
	"testing"
	"time"
)

func TestNewAzureBlobSourceFromMap(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
		check   func(*testing.T, Source)
	}{
		{
			name: "valid with connection string",
			cfg: map[string]interface{}{
				"container":         "configs",
				"blob_name":         "redirector.yaml",
				"connection_string": "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=dGVzdA==;EndpointSuffix=core.windows.net",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				ab, _ := s.(*AzureBlobSource)
				if ab.cfg.Container != "configs" {
					t.Errorf("container = %q, want %q", ab.cfg.Container, "configs")
				}
				if ab.cfg.BlobName != "redirector.yaml" {
					t.Errorf("blob_name = %q, want %q", ab.cfg.BlobName, "redirector.yaml")
				}
			},
		},
		{
			name: "valid with account key",
			cfg: map[string]interface{}{
				"storage_account": "mystorageaccount",
				"container":       "configs",
				"blob_name":       "config.yaml",
				"account_key":     "dGVzdGtleQ==",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				ab, _ := s.(*AzureBlobSource)
				if ab.cfg.StorageAccount != "mystorageaccount" {
					t.Errorf("storage_account = %q, want %q", ab.cfg.StorageAccount, "mystorageaccount")
				}
			},
		},
		{
			name: "valid with SAS token",
			cfg: map[string]interface{}{
				"storage_account": "mystorageaccount",
				"container":       "configs",
				"blob_name":       "config.yaml",
				"sas_token":       "sv=2021-06-08&ss=b&srt=co&sp=r&se=2025-01-01&st=2024-01-01&spr=https&sig=xxxxx",
			},
			wantErr: false,
		},
		{
			name: "valid with poll interval",
			cfg: map[string]interface{}{
				"container":         "configs",
				"blob_name":         "config.yaml",
				"connection_string": "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=dGVzdA==;EndpointSuffix=core.windows.net",
				"poll_interval":     "10m",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				ab, _ := s.(*AzureBlobSource)
				if ab.cfg.PollInterval != 10*time.Minute {
					t.Errorf("poll_interval = %v, want 10m", ab.cfg.PollInterval)
				}
			},
		},
		{
			name: "missing container",
			cfg: map[string]interface{}{
				"blob_name":         "config.yaml",
				"connection_string": "conn",
			},
			wantErr: true,
		},
		{
			name: "missing blob_name",
			cfg: map[string]interface{}{
				"container":         "configs",
				"connection_string": "conn",
			},
			wantErr: true,
		},
		{
			name: "no auth method",
			cfg: map[string]interface{}{
				"container": "configs",
				"blob_name": "config.yaml",
			},
			wantErr: true,
		},
		{
			name: "account key without storage account",
			cfg: map[string]interface{}{
				"container":   "configs",
				"blob_name":   "config.yaml",
				"account_key": "key",
			},
			wantErr: true,
		},
		{
			name: "SAS token without storage account",
			cfg: map[string]interface{}{
				"container": "configs",
				"blob_name": "config.yaml",
				"sas_token": "token",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewAzureBlobSourceFromMap(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewAzureBlobSourceFromMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && source != nil {
				tt.check(t, source)
			}
		})
	}
}

func TestAzureBlobSource_Name(t *testing.T) {
	source := &AzureBlobSource{
		cfg: AzureBlobSourceConfig{
			Container: "configs",
			BlobName:  "config.yaml",
		},
	}

	if source.Name() != "azureblob" {
		t.Errorf("Name() = %q, want %q", source.Name(), "azureblob")
	}
}

func TestAzureBlobSource_SupportsWatch(t *testing.T) {
	source := &AzureBlobSource{}

	if !source.SupportsWatch() {
		t.Error("SupportsWatch() = false, want true")
	}
}

func TestAzureBlobSource_Registry(t *testing.T) {
	// Verify Azure Blob source is registered
	factory, ok := Registry.Get("azureblob")
	if !ok {
		t.Fatal("azureblob source not registered")
	}

	// Can't test full creation without valid connection string
	_ = factory
}
