# R.I.T.U.A.L. Defensive Programming Standards
# NASA JPL Power of Ten Compliance

## Overview

R.I.T.U.A.L. enforces NASA JPL Power of Ten defensive programming standards for mission-critical reliability. These standards ensure code robustness, predictability, and maintainability in high-stakes environments.

**Reference**: [NASA JPL Power of Ten Rules](https://spinroot.com/static10/Src/DOC/PowerOfTen.pdf)

## Mandatory Rules

### 1. Nil Validation

**Rule**: All constructors and functions accepting interface/pointer parameters MUST validate non-nil before use. Return error, not panic.

**Correct Pattern**:
```go
func NewLibrarianService(storage StorageRepository) (*LibrarianService, error) {
    if storage == nil {
        return nil, errors.New("storage repository cannot be nil")
    }
    return &LibrarianService{storage: storage}, nil
}

func (l *LibrarianService) GetManifest(ctx context.Context) (*domain.Manifest, error) {
    if ctx == nil {
        return nil, errors.New("context cannot be nil")
    }
    // Function implementation
}
```

**Prohibited Pattern**:
```go
func NewLibrarianService(storage StorageRepository) *LibrarianService {
    return &LibrarianService{storage: storage} // No nil check
}
```

### 2. Runtime Assertions

**Rule**: Average ≥2 assertions per function. Check preconditions, postconditions, invariants.

**Correct Pattern**:
```go
func (m *Manifest) Lock(lockBy string) error {
    // Precondition assertion
    if lockBy == "" {
        return errors.New("lockBy cannot be empty")
    }
    
    // Business logic
    m.LockedBy = lockBy
    m.UpdatedAt = time.Now()
    
    // Postcondition assertion
    if m.LockedBy == "" {
        return errors.New("lock operation failed")
    }
    
    return nil
}
```

### 3. Error Handling

**Rule**: Check ALL return values from non-void functions. No ignored errors.

**Correct Pattern**:
```go
func (s *StorageService) SaveManifest(ctx context.Context, manifest *domain.Manifest) error {
    data, err := json.Marshal(manifest)
    if err != nil {
        return fmt.Errorf("failed to marshal manifest: %w", err)
    }
    
    err = s.repository.Put(ctx, "manifest.json", data)
    if err != nil {
        return fmt.Errorf("failed to save manifest: %w", err)
    }
    
    return nil
}
```

**Prohibited Pattern**:
```go
func (s *StorageService) SaveManifest(ctx context.Context, manifest *domain.Manifest) error {
    data, _ := json.Marshal(manifest) // Ignored error
    s.repository.Put(ctx, "manifest.json", data) // Ignored error
    return nil
}
```

### 4. Input Validation

**Rule**: Validate all function parameters at entry. Check ranges, bounds, nil pointers.

**Correct Pattern**:
```go
func (v *ValidatorService) CheckInstance(local, remote *domain.Manifest) error {
    // Input validation
    if local == nil {
        return errors.New("local manifest cannot be nil")
    }
    if remote == nil {
        return errors.New("remote manifest cannot be nil")
    }
    if local.InstanceID == "" {
        return errors.New("local instance ID cannot be empty")
    }
    if remote.InstanceID == "" {
        return errors.New("remote instance ID cannot be empty")
    }
    
    // Business logic
    if local.InstanceID != remote.InstanceID {
        return errors.New("instance ID mismatch")
    }
    
    return nil
}
```

### 5. Fixed Bounds

**Rule**: All loops must have statically determinable upper bounds.

**Correct Pattern**:
```go
func (l *LibrarianService) ProcessWorlds(worlds []domain.World) error {
    const maxWorlds = 100 // Static bound
    if len(worlds) > maxWorlds {
        return errors.New("too many worlds to process")
    }
    
    for i := 0; i < len(worlds); i++ { // Bounded loop
        err := l.processWorld(&worlds[i])
        if err != nil {
            return fmt.Errorf("failed to process world %d: %w", i, err)
        }
    }
    
    return nil
}
```

**Prohibited Pattern**:
```go
func (l *LibrarianService) ProcessWorlds(worlds []domain.World) error {
    for _, world := range worlds { // No explicit bound check
        err := l.processWorld(&world)
        if err != nil {
            return err
        }
    }
    return nil
}
```

### 6. Function Size

**Rule**: Limit functions to 60 lines. Extract complex logic.

**Correct Pattern**:
```go
func (m *MolfarService) Prepare() error {
    err := m.validateEnvironment()
    if err != nil {
        return fmt.Errorf("environment validation failed: %w", err)
    }
    
    err = m.initializeStorage()
    if err != nil {
        return fmt.Errorf("storage initialization failed: %w", err)
    }
    
    return m.setupManifest()
}

func (m *MolfarService) validateEnvironment() error {
    // Validation logic (extracted)
    return nil
}

func (m *MolfarService) initializeStorage() error {
    // Storage initialization (extracted)
    return nil
}
```

### 7. Pointer Safety

**Rule**: Minimize pointer indirection. Validate before dereferencing.

**Correct Pattern**:
```go
func (m *Manifest) GetLatestWorld() (*domain.World, error) {
    if len(m.StoredWorlds) == 0 {
        return nil, errors.New("no worlds available")
    }
    
    latest := &m.StoredWorlds[len(m.StoredWorlds)-1]
    if latest == nil {
        return nil, errors.New("latest world is nil")
    }
    
    return latest, nil
}
```

### 8. Memory Safety

**Rule**: No dynamic heap allocation after initialization phase.

**Correct Pattern**:
```go
type WorldProcessor struct {
    buffer [1024]byte // Pre-allocated buffer
    worlds []domain.World // Pre-allocated slice
}

func NewWorldProcessor() *WorldProcessor {
    return &WorldProcessor{
        worlds: make([]domain.World, 0, 100), // Pre-allocate capacity
    }
}
```

### 9. Compiler Warnings

**Rule**: Enable all warnings (-Wall -Wextra). Zero tolerance for warnings.

**Build Configuration**:
```bash
go build -ldflags="-w -s" -gcflags="all=-N -l" ./cmd/cli
```

**Static Analysis Tools**:
```bash
# Run all static analysis tools
golangci-lint run --enable-all
go vet ./...
staticcheck ./...
```

### 10. Static Analysis

**Rule**: Run static analysis tools. Fix all findings before merge.

**Required Tools**:
- `golangci-lint` with all linters enabled
- `go vet` for basic static analysis
- `staticcheck` for advanced static analysis
- `gosec` for security analysis

## Implementation Examples

### Service Constructor Pattern

```go
func NewMolfarService(
    librarian LibrarianService,
    validator ValidatorService,
    storage StorageRepository,
) (*MolfarService, error) {
    // Nil validation
    if librarian == nil {
        return nil, errors.New("librarian service cannot be nil")
    }
    if validator == nil {
        return nil, errors.New("validator service cannot be nil")
    }
    if storage == nil {
        return nil, errors.New("storage repository cannot be nil")
    }
    
    // Precondition assertion
    if !isValidConfiguration() {
        return nil, errors.New("invalid configuration")
    }
    
    service := &MolfarService{
        librarian: librarian,
        validator: validator,
        storage:   storage,
    }
    
    // Postcondition assertion
    if service.librarian == nil {
        return nil, errors.New("service initialization failed")
    }
    
    return service, nil
}
```

### Error Propagation Pattern

```go
func (l *LibrarianService) SynchronizeManifests(ctx context.Context) error {
    // Input validation
    if ctx == nil {
        return errors.New("context cannot be nil")
    }
    
    // Get local manifest with error handling
    local, err := l.GetLocalManifest(ctx)
    if err != nil {
        return fmt.Errorf("failed to get local manifest: %w", err)
    }
    
    // Get remote manifest with error handling
    remote, err := l.GetRemoteManifest(ctx)
    if err != nil {
        return fmt.Errorf("failed to get remote manifest: %w", err)
    }
    
    // Validate manifests
    err = l.validateManifests(local, remote)
    if err != nil {
        return fmt.Errorf("manifest validation failed: %w", err)
    }
    
    // Synchronize with error handling
    err = l.performSynchronization(ctx, local, remote)
    if err != nil {
        return fmt.Errorf("synchronization failed: %w", err)
    }
    
    return nil
}
```

### Assertion Pattern

```go
func (m *Manifest) AddWorld(world domain.World) error {
    // Precondition assertions
    if world.URI == "" {
        return errors.New("world URI cannot be empty")
    }
    if world.CreatedAt.IsZero() {
        return errors.New("world creation time cannot be zero")
    }
    
    // Business logic
    m.StoredWorlds = append(m.StoredWorlds, world)
    m.UpdatedAt = time.Now()
    
    // Postcondition assertions
    if len(m.StoredWorlds) == 0 {
        return errors.New("world addition failed")
    }
    if m.UpdatedAt.IsZero() {
        return errors.New("update time not set")
    }
    
    return nil
}
```

## Testing Requirements

### Defensive Testing Patterns

```go
func TestLibrarianService_NilValidation(t *testing.T) {
    tests := []struct {
        name    string
        storage StorageRepository
        wantErr bool
    }{
        {
            name:    "nil storage",
            storage: nil,
            wantErr: true,
        },
        {
            name:    "valid storage",
            storage: &MockStorageRepository{},
            wantErr: false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            service, err := NewLibrarianService(tt.storage)
            if (err != nil) != tt.wantErr {
                t.Errorf("NewLibrarianService() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !tt.wantErr && service == nil {
                t.Error("NewLibrarianService() returned nil service")
            }
        })
    }
}
```

## Compliance Verification

### Pre-commit Checklist

- [ ] All functions validate nil parameters
- [ ] All functions include ≥2 assertions
- [ ] All error returns are checked
- [ ] All loops have static bounds
- [ ] All functions ≤60 lines
- [ ] No dynamic allocations after init
- [ ] Zero compiler warnings
- [ ] All static analysis tools pass

### Continuous Integration

```yaml
# .github/workflows/ci.yml
- name: Run Static Analysis
  run: |
    golangci-lint run --enable-all
    go vet ./...
    staticcheck ./...
    gosec ./...
```

## References

- [NASA JPL Power of Ten Rules](https://spinroot.com/static10/Src/DOC/PowerOfTen.pdf)
- [Go Error Handling Best Practices](https://go.dev/blog/error-handling-and-go)
- [Static Analysis Tools for Go](https://golang.org/cmd/vet/)
- [Defensive Programming Principles](https://en.wikipedia.org/wiki/Defensive_programming)
