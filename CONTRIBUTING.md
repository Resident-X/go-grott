# Contributing to go-grott

Thank you for your interest in contributing to go-grott! This document provides guidelines and information for contributors.

## 🚀 Getting Started

### Prerequisites

- Go 1.24 or later
- [Task runner](https://taskfile.dev/) (recommended)
- [Mockery](https://vektra.github.io/mockery/) (for mock generation)
- Git

### Development Setup

1. **Fork and Clone**
   ```bash
   git clone https://github.com/YOUR_USERNAME/go-grott.git
   cd go-grott
   ```

2. **Install Development Tools**
   ```bash
   # Install Task runner
   go install github.com/go-task/task/v3/cmd/task@latest
   
   # Install Mockery for generating test mocks
   go install github.com/vektra/mockery/v2@latest
   
   # Install golangci-lint (optional, for advanced linting)
   go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
   ```

3. **Verify Setup**
   ```bash
   task deps      # Download dependencies
   task build     # Build the application
   task test      # Run tests
   ```

## 🔄 Development Workflow

### Branch Strategy (Trunk-Based Development)

- `main` - Primary development branch (always deployable)
- Feature branches: `feature/description` (short-lived, merge to main)
- Bug fix branches: `fix/description` (short-lived, merge to main)
- Hot fix branches: `hotfix/v1.2.4` (for critical production fixes)

**Key Principles:**
- Keep feature branches small and short-lived (< 2 days)
- `main` branch is always in a deployable state
- All changes go through `main` branch
- Create releases from `main` as needed
- Use feature flags for incomplete features if necessary

### Making Changes

1. **Create a Feature Branch from Main**
   ```bash
   git checkout main
   git pull origin main
   git checkout -b feature/your-feature-name
   ```

2. **Development Loop**
   ```bash
   # Make your changes (keep them small and focused)
   task dev          # Run in development mode
   task test         # Run tests frequently
   task check        # Validate code quality
   ```

3. **Before Committing**
   ```bash
   task check        # Runs fmt, vet, and tests
   task coverage     # Ensure test coverage remains high
   task mocks        # Regenerate mocks if interfaces changed
   ```

4. **Commit Standards**
   ```bash
   # Use conventional commit format
   git commit -m "feat: add new HTTP API endpoint"
   git commit -m "fix: resolve parsing issue with layout X"
   git commit -m "docs: update configuration examples"
   ```

5. **Merge to Main**
   ```bash
   # Rebase on main before creating PR
   git checkout main
   git pull origin main
   git checkout feature/your-feature-name
   git rebase main
   
   # Create PR to merge into main
   # After approval and merge, delete feature branch
   git checkout main
   git pull origin main
   git branch -d feature/your-feature-name
   ```

### Release Process

1. **Creating a Release**
   ```bash
   # Create release branch from main
   git checkout main
   git pull origin main
   git checkout -b release/v1.2.3
   
   # Update version information
   # Create release tag
   git tag -a v1.2.3 -m "Release v1.2.3"
   git push origin v1.2.3
   ```

2. **Hot Fixes**
   ```bash
   # For critical production fixes
   git checkout -b hotfix/v1.2.4 v1.2.3
   # Make fix
   # Tag new version
   git tag -a v1.2.4 -m "Hotfix v1.2.4"
   # Merge back to main
   ```

## 📋 Code Standards

### Go Code Style

- Follow standard Go formatting (`gofmt`)
- Use meaningful variable and function names
- Add comments for exported functions and types
- Keep functions focused and concise
- Handle errors appropriately

### Architecture Principles

- **Clean Architecture**: Maintain separation between layers
- **Interface-First**: Define interfaces before implementations
- **Dependency Injection**: Avoid tight coupling between components
- **Testability**: Write testable code with proper mocking
- **Single Responsibility**: Each component should have one clear purpose

### Example Code Structure

```go
// Good: Interface-based design with clear separation
type DataParser interface {
    ParsePacket(data []byte) (*domain.InverterData, error)
}

type GrowattParser struct {
    logger    zerolog.Logger
    layouts   map[string]*Layout
    validator validation.Validator
}

func NewGrowattParser(
    logger zerolog.Logger,
    layouts map[string]*Layout,
    validator validation.Validator,
) *GrowattParser {
    return &GrowattParser{
        logger:    logger.With().Str("component", "parser").Logger(),
        layouts:   layouts,
        validator: validator,
    }
}
```

## 🧪 Testing Requirements

### Test Coverage Standards

- **Minimum Coverage**: 80% overall
- **New Code**: 90% coverage required
- **Critical Components**: 95%+ coverage (API, protocol handling)

### Test Types

1. **Unit Tests**: Test individual components in isolation
   ```bash
   task test-unit
   ```

2. **Integration Tests**: Test component interactions
   ```bash
   task test-integration
   ```

3. **End-to-End Tests**: Test complete system workflows
   ```bash
   task test-e2e
   ```

### Writing Tests

```go
// Example unit test with mocks
func TestGrowattParser_ParsePacket(t *testing.T) {
    // Arrange
    mockValidator := &mocks.Validator{}
    parser := NewGrowattParser(
        zerolog.New(os.Stdout),
        testLayouts,
        mockValidator,
    )
    
    testData := []byte{0x68, 0x65, 0x6c, 0x6c, 0x6f}
    
    mockValidator.On("ValidatePacket", testData).Return(&validation.ValidationResult{
        IsValid: true,
        Confidence: 0.95,
    }, nil)
    
    // Act
    result, err := parser.ParsePacket(testData)
    
    // Assert
    require.NoError(t, err)
    assert.NotNil(t, result)
    mockValidator.AssertExpectations(t)
}
```

### Mock Generation

When adding or modifying interfaces:

```bash
# Regenerate all mocks
task mocks-clean

# Or regenerate specific mocks
mockery --name=YourInterface --dir=./internal/domain --output=./mocks
```

## 📝 Documentation

### Code Documentation

- Document all exported functions and types
- Include examples for complex functionality
- Keep documentation up-to-date with code changes

### README and Docs

- Update README.md for user-facing changes
- Add configuration examples for new features
- Update architecture documentation for significant changes

## 🚀 Pull Request Process

### Before Submitting

1. **Code Quality**
   ```bash
   task check        # Format, vet, and test
   task coverage     # Verify coverage
   task mocks        # Update mocks if needed
   ```

2. **Documentation**
   - Update relevant documentation
   - Add configuration examples if applicable
   - Update API documentation for new endpoints

3. **Testing**
   ```bash
   task test-all     # Run complete test suite
   ```

### Pull Request Template

```markdown
## Description
Brief description of changes and motivation.

## Type of Change
- [ ] Bug fix (non-breaking change)
- [ ] New feature (non-breaking change)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work)
- [ ] Documentation update

## Testing
- [ ] Unit tests added/updated
- [ ] Integration tests added/updated
- [ ] Manual testing completed
- [ ] Test coverage maintained/improved

## Checklist
- [ ] Code follows project style guidelines
- [ ] Self-review completed
- [ ] Documentation updated
- [ ] No new warnings or errors introduced
```

### Review Process

1. **Automated Checks**: All CI/CD checks must pass
2. **Code Review**: At least one maintainer review required
3. **Integration**: Feature branch must be up-to-date with `main`
4. **Testing**: All tests must pass, coverage maintained
5. **Merge Strategy**: 
   - Small features: Squash and merge to `main`
   - Larger features: Rebase and merge to maintain history
6. **Cleanup**: Delete feature branch after successful merge

### Trunk-Based Benefits
- **Faster Integration**: Changes integrate continuously
- **Reduced Merge Conflicts**: Smaller, more frequent merges
- **Simplified Process**: No complex branching strategies  
- **Always Deployable**: `main` branch is always release-ready
- **Rapid Feedback**: Quick validation of changes

## 🐛 Bug Reports

### Before Reporting

1. Check existing issues
2. Verify with latest version
3. Collect relevant information:
   - Go version
   - Operating system
   - Configuration (sanitized)
   - Log output
   - Steps to reproduce

### Bug Report Template

```markdown
## Bug Description
Clear description of the issue.

## Environment
- OS: [e.g., Ubuntu 22.04]
- Go Version: [e.g., 1.24.1]
- go-grott Version: [e.g., v1.2.3]

## Steps to Reproduce
1. Step one
2. Step two
3. Step three

## Expected Behavior
What should have happened.

## Actual Behavior
What actually happened.

## Logs
Relevant log output (sanitize sensitive data).

## Additional Context
Any other relevant information.
```

## 💡 Feature Requests

### Before Requesting

1. Check existing feature requests
2. Consider if it fits the project scope
3. Think about implementation approach

### Feature Request Template

```markdown
## Feature Description
Clear description of the proposed feature.

## Use Case
Why is this feature needed? What problem does it solve?

## Proposed Implementation
How might this feature be implemented?

## Alternatives
What alternatives have you considered?

## Additional Context
Any other relevant information.
```

## 📞 Getting Help

- **GitHub Issues**: For bugs and feature requests
- **GitHub Discussions**: For questions and general discussion
- **Code Review**: Tag maintainers for review assistance

## 🎯 Development Focus Areas

We particularly welcome contributions in these areas:

- **Protocol Support**: Adding support for new Growatt inverter models
- **Integrations**: New monitoring service integrations
- **Performance**: Optimizations for high-throughput scenarios
- **Documentation**: Improving user guides and API documentation
- **Testing**: Expanding test coverage and scenarios

## 📄 License

By contributing, you agree that your contributions will be licensed under the MIT License.

## 🙏 Recognition

Contributors will be recognized in:
- README.md acknowledgments
- CHANGELOG.md for their contributions
- GitHub contributors page

Thank you for contributing to go-grott! 🚀
