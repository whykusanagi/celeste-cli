package skills

import (
	"context"
	"testing"

	ipath "github.com/ipfs/boxo/coreiface/path"
	"github.com/ipfs/boxo/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockIPFSClient struct {
	addCID    string
	addErr    error
	getNode   files.Node
	getErr    error
	pinAddErr error
	pinRmErr  error
	listPins  []string
	listErr   error
}

func (m *mockIPFSClient) Add(ctx context.Context, file files.Node) (string, error) {
	return m.addCID, m.addErr
}

func (m *mockIPFSClient) Get(ctx context.Context, path ipath.Path) (files.Node, error) {
	return m.getNode, m.getErr
}

func (m *mockIPFSClient) PinAdd(ctx context.Context, path ipath.Path) error {
	return m.pinAddErr
}

func (m *mockIPFSClient) PinRm(ctx context.Context, path ipath.Path) error {
	return m.pinRmErr
}

func (m *mockIPFSClient) ListPins(ctx context.Context) ([]string, error) {
	return m.listPins, m.listErr
}

func TestResolveIPFSEndpoint(t *testing.T) {
	assert.Equal(t, "/dns/ipfs.infura.io/tcp/5001/https", resolveIPFSEndpoint(IPFSConfig{Provider: "infura"}))
	assert.Equal(t, "/dns/api.pinata.cloud/tcp/443/https", resolveIPFSEndpoint(IPFSConfig{Provider: "pinata"}))
	assert.Equal(t, "/ip4/127.0.0.1/tcp/5001", resolveIPFSEndpoint(IPFSConfig{Provider: "other"}))
	assert.Equal(t, "/dns/custom/tcp/443/https", resolveIPFSEndpoint(IPFSConfig{GatewayURL: "/dns/custom/tcp/443/https"}))
}

func TestApplyIPFSAuthHeaders(t *testing.T) {
	headers := map[string]string{}
	addHeader := func(key, value string) {
		headers[key] = value
	}

	applyIPFSAuthHeaders(IPFSConfig{
		Provider:  "infura",
		ProjectID: "pid",
		APISecret: "secret",
	}, addHeader)

	assert.Contains(t, headers["Authorization"], "Basic ")

	headers = map[string]string{}
	applyIPFSAuthHeaders(IPFSConfig{
		Provider:  "pinata",
		APIKey:    "key123",
		APISecret: "secret123",
	}, addHeader)

	assert.Equal(t, "key123", headers["pinata_api_key"])
	assert.Equal(t, "secret123", headers["pinata_secret_api_key"])
}

func TestCreateIPFSClientInvalidEndpoint(t *testing.T) {
	_, err := createIPFSClient(IPFSConfig{
		Provider:   "custom",
		GatewayURL: "not-a-multiaddr",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid IPFS endpoint")
}

func TestHandleIPFSUploadValidation(t *testing.T) {
	client := &mockIPFSClient{}

	resp, err := handleIPFSUpload(context.Background(), client, map[string]interface{}{}, IPFSConfig{})
	require.NoError(t, err)

	body := resp.(map[string]interface{})
	assert.True(t, body["error"].(bool))
	assert.Equal(t, "validation_error", body["error_type"])
}

func TestHandleIPFSUploadSuccessWithContent(t *testing.T) {
	client := &mockIPFSClient{addCID: "bafkreiabc123"}

	resp, err := handleIPFSUpload(context.Background(), client, map[string]interface{}{
		"content": "hello world",
	}, IPFSConfig{})
	require.NoError(t, err)

	body := resp.(map[string]interface{})
	assert.True(t, body["success"].(bool))
	assert.Equal(t, "bafkreiabc123", body["cid"])
	assert.Equal(t, "content", body["type"])
}

func TestHandleIPFSDownloadInvalidCID(t *testing.T) {
	client := &mockIPFSClient{}

	resp, err := handleIPFSDownload(context.Background(), client, map[string]interface{}{
		"cid": "not-a-cid",
	})
	require.NoError(t, err)

	body := resp.(map[string]interface{})
	assert.True(t, body["error"].(bool))
	assert.Equal(t, "validation_error", body["error_type"])
}

func TestHandleIPFSPinInvalidCID(t *testing.T) {
	client := &mockIPFSClient{}

	resp, err := handleIPFSPin(context.Background(), client, map[string]interface{}{
		"cid": "not-a-cid",
	})
	require.NoError(t, err)

	body := resp.(map[string]interface{})
	assert.True(t, body["error"].(bool))
	assert.Equal(t, "validation_error", body["error_type"])
}

func TestHandleIPFSUnpinInvalidCID(t *testing.T) {
	client := &mockIPFSClient{}

	resp, err := handleIPFSUnpin(context.Background(), client, map[string]interface{}{
		"cid": "not-a-cid",
	})
	require.NoError(t, err)

	body := resp.(map[string]interface{})
	assert.True(t, body["error"].(bool))
	assert.Equal(t, "validation_error", body["error_type"])
}

func TestHandleIPFSListPins(t *testing.T) {
	client := &mockIPFSClient{
		listPins: []string{"bafy1", "bafy2"},
	}

	resp, err := handleIPFSListPins(context.Background(), client)
	require.NoError(t, err)

	body := resp.(map[string]interface{})
	assert.True(t, body["success"].(bool))
	assert.Equal(t, 2, body["count"])
}

func TestIPFSHandlerUnknownOperation(t *testing.T) {
	loader := NewMockConfigLoader()
	oldFactory := newIPFSClient
	newIPFSClient = func(config IPFSConfig) (ipfsClient, error) {
		return &mockIPFSClient{}, nil
	}
	t.Cleanup(func() {
		newIPFSClient = oldFactory
	})

	resp, err := IPFSHandler(map[string]interface{}{
		"operation": "wat",
	}, loader)
	require.NoError(t, err)

	body := resp.(map[string]interface{})
	assert.True(t, body["error"].(bool))
	assert.Equal(t, "validation_error", body["error_type"])
}
