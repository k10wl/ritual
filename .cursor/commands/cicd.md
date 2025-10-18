---
description: AI-executed CI/CD pipeline for R.I.T.U.A.L. project
globs: ["**/*.go", "**/*.md", "docs/**"]
alwaysApply: false
---

# R.I.T.U.A.L. CI/CD Pipeline

AI-executed comprehensive CI/CD pipeline that reviews code quality, verifies documentation, runs tests, and creates conventional commits.

## Pipeline Overview

This AI command executes a complete CI/CD workflow through the following phases:

### Phase 1: Pre-Flight Checks
- **Git Status Analysis**: Examine working directory status and staged changes
- **Branch Analysis**: Review current branch name and git history
- **Dependency Validation**: Check go.mod and go.sum for dependency integrity

### Phase 2: Build & Cleanup
- **Build Validation**: Execute `go build ./...` to verify code compiles successfully
- **Cleanup Operations**: Run `go clean ./...` and `go mod tidy` to clean build artifacts
- **Dependency Verification**: Ensure all dependencies are properly resolved and up-to-date
- **Build Artifacts**: Remove any stale build artifacts that might cause issues

### Phase 3: Code Quality & Security Review
- **Built-in Go Analysis**: Execute `go vet ./...` for basic static analysis
- **Semantic Code Review**: AI analysis of code quality, patterns, and potential issues
- **Code Duplication Detection**: AI-powered analysis of duplicate patterns and refactoring opportunities
- **Security Analysis**: AI review for common Go security patterns and vulnerabilities
- **NASA JPL Compliance**: AI verification of Power of Ten defensive programming standards
- **Architecture Compliance**: Cross-reference code against `docs/structure.md`, `docs/overview.md`, `docs/coding-practices.md`
- **Code Quality Metrics**: AI analysis of complexity, maintainability, and design patterns
- **🔍 Interactive Review**: Present findings with severity levels and **prompt user** for continuation

### Phase 4: Documentation Integrity
- **Progress Tracking**: Compare `docs/progress.md` against actual implementation state
- **Architecture Alignment**: Verify code structure matches documented architecture
- **API Documentation**: Check GoDoc comments completeness and accuracy
- **Auto-Update**: Update documentation if discrepancies found (with user approval)

### Phase 5: Testing & Coverage Analysis
- **Intelligent Test Execution**: Execute `go test -v -cover ./...` in a single batch operation
- **Comprehensive Coverage**: Generate and analyze coverage data in one pass
- **Test Quality Review**: AI analysis of test completeness and quality
- **Integration Validation**: Verify component integration through AI analysis
- **📊 Coverage Analysis**: AI-powered coverage analysis and recommendations
- **🚫 STRICT PROHIBITION**: Multiple test runs are strictly prohibited - execute tests exactly once only
- **🎯 Single Pass**: Run all tests once intelligently - no retry loops, no multiple executions, no iterations

### Phase 6: Commit Message Intelligence
- **Change Analysis**: Analyze git diff since branch creation
- **Conventional Commit**: Generate appropriate commit type (feat, fix, refactor, etc.)
- **Scope Detection**: Identify affected components and modules
- **Breaking Changes**: Detect and flag breaking changes
- **Message Templates**: Use project-specific templates for consistency
- **🔄 Iterative Refinement**: Review and refine message until optimal
- **✅ User Approval**: Present final message for confirmation

### Phase 7: Commit Execution
- **Staging**: Intelligently stage only relevant files
- **Commit Creation**: Execute `git commit` with approved message
- **Post-Commit**: Update progress tracking and generate commit summary

## Advanced Features

### Intelligent Error Recovery
- **Rollback Capability**: Ability to undo changes if pipeline fails
- **Resume Points**: Continue from failure points without restarting
- **Incremental Progress**: Save state at each phase for recovery

### Customization Options
- **Quality Gates**: AI-powered quality assessment with configurable thresholds
- **Test Coverage**: AI analysis of test coverage adequacy
- **Commit Templates**: Customizable commit message formats
- **Skip Options**: Ability to skip specific phases if needed

### Integration Points
- **Pre-commit Hooks**: Automatic execution of quality checks
- **CI/CD Integration**: Compatible with GitHub Actions, GitLab CI, etc.
- **IDE Integration**: Works seamlessly with Cursor and VS Code

## Tool Requirements

### Required (Built-in)
- **Go 1.25+**: Built-in `go test`, `go vet`, `go build`, `go mod` commands
- **Git**: For version control and commit operations
- **Cursor AI**: For semantic analysis and code review


### AI-Powered Analysis
This pipeline leverages Cursor's AI capabilities for:
- Code quality assessment
- Duplication detection
- Security pattern analysis
- Architecture compliance verification
- Test coverage analysis
- Commit message generation

The pipeline gracefully degrades if external tools are unavailable, relying on built-in Go tools and AI analysis.

## Safety Features

- **🛡️ User Prompts**: All critical actions require explicit user confirmation
- **📋 Audit Trail**: Complete log of all actions taken during pipeline execution
- **🔄 Rollback**: Ability to undo changes if issues are discovered
- **⏸️ Pause Points**: Option to pause and review at each major phase
- **🔍 Transparency**: Detailed reporting of all checks and findings

## Usage

This AI command will execute the complete CI/CD pipeline when invoked. The AI will guide you through each step and ask for confirmation before proceeding with critical actions.

The pipeline is designed to be interactive, with user prompts at key decision points to ensure you maintain control over the process.
