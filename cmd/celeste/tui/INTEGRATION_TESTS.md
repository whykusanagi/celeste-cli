# Collections TUI Integration Tests

This directory contains integration tests for the Collections TUI that make real API calls to xAI.

## Running Integration Tests

Integration tests are tagged with `// +build integration` and are **not run by default** with `go test`.

### Prerequisites

1. **xAI Management API Key** must be configured:
   ```bash
   celeste config --set-management-key xai-YOUR-KEY
   # OR
   export XAI_MANAGEMENT_API_KEY=xai-YOUR-KEY
   ```

2. **Active internet connection** (tests make real API calls)

3. **At least one collection** should exist (tests will skip if no collections found)

### Run Integration Tests

```bash
# Run all integration tests in this package
go test -tags=integration ./cmd/celeste/tui -v

# Run specific test
go test -tags=integration ./cmd/celeste/tui -run TestCollectionsModel_Integration -v

# Run with timeout
go test -tags=integration ./cmd/celeste/tui -v -timeout 30s
```

### What Gets Tested

#### 1. Model Initialization
- Loads collections from xAI API
- Handles API responses correctly
- Populates model with real data

#### 2. View Rendering
- Renders all UI elements
- Displays active/inactive collections
- Shows navigation hints
- Formats output correctly

#### 3. Navigation
- Up/Down arrow keys
- k/j vim-style keys
- Cursor movement
- Boundary handling

#### 4. Toggle Functionality
- Space key enables/disables collections
- Active count changes correctly
- Config persisted to disk
- Changes reload correctly

#### 5. Error Handling
- Empty collections list
- API errors
- Invalid responses

### Test Output Example

```
=== RUN   TestCollectionsModel_Integration
=== RUN   TestCollectionsModel_Integration/ModelInitialization
    collections_integration_test.go:50: Successfully loaded 10 collections
=== RUN   TestCollectionsModel_Integration/ViewRendering
    collections_integration_test.go:84: View rendered successfully with 752 characters
=== RUN   TestCollectionsModel_Integration/Navigation
    collections_integration_test.go:130: All navigation keys working correctly
=== RUN   TestCollectionsModel_Integration/ToggleFunctionality
    collections_integration_test.go:178: Toggle successful: 1 -> 2 active collections
--- PASS: TestCollectionsModel_Integration (2.45s)
    --- PASS: TestCollectionsModel_Integration/ModelInitialization (0.82s)
    --- PASS: TestCollectionsModel_Integration/ViewRendering (0.76s)
    --- PASS: TestCollectionsModel_Integration/Navigation (0.05s)
    --- PASS: TestCollectionsModel_Integration/ToggleFunctionality (0.82s)
=== RUN   TestCollectionsModel_EmptyState
--- PASS: TestCollectionsModel_EmptyState (0.00s)
=== RUN   TestCollectionsModel_ErrorState
--- PASS: TestCollectionsModel_ErrorState (0.00s)
PASS
ok      github.com/whykusanagi/celesteCLI/cmd/celeste/tui       2.451s
```

### Skipped Tests

Tests will be skipped if:
- No management API key configured
- No collections exist (for navigation/toggle tests)
- API is unavailable

### Notes

- **Tests modify config**: Toggle tests save config changes to disk
- **API rate limits**: May apply depending on xAI account
- **Real collections**: Tests use your actual collections (non-destructive)
- **Cleanup**: Tests restore original state after toggle operations

### Troubleshooting

**"xAI Management API key not configured"**
```bash
celeste config --set-management-key xai-YOUR-KEY
```

**"Need at least 2 collections to test navigation"**
```bash
celeste collections create "test-1" --description "Test collection"
celeste collections create "test-2" --description "Another test"
```

**Timeout errors**
```bash
# Increase timeout for slow connections
go test -tags=integration ./cmd/celeste/tui -v -timeout 60s
```

### CI/CD Integration

To run integration tests in CI/CD, ensure:
1. `XAI_MANAGEMENT_API_KEY` environment variable is set
2. Build tag `integration` is included
3. Sufficient timeout is configured

Example GitHub Actions:
```yaml
- name: Run Integration Tests
  env:
    XAI_MANAGEMENT_API_KEY: ${{ secrets.XAI_MANAGEMENT_API_KEY }}
  run: go test -tags=integration ./cmd/celeste/tui -v -timeout 30s
```
