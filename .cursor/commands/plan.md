# AI Code Review Command

## Command
```
/review [files] [focus]
```

## Parameters
- **files**: `all`, `modified`, `file:path`, `test:pattern`
- **focus**: `arch`, `test`, `perf`, `sec`, `maintain`, `consistency`

## Examples
```
/review modified test
/review all arch
/review file:backupper.go maintain
```

## Output Format
```
## Review: [scope] - [focus]

### Issues
- [Critical/Major/Minor] Description

### Recommendations
- Action item

### Score: X/10
```

## Focus Definitions
- `arch` - Hexagonal architecture compliance
- `test` - Test coverage and quality
- `perf` - Performance bottlenecks
- `sec` - Security vulnerabilities
- `maintain` - Code clarity and complexity
- `consistency` - Style and patterns

---

# AI Plan Command

## Command
```
/plan [user_requirements]
```

## Parameters
- **user_requirements**: Natural language description of what needs to be done

## Examples
```
/plan "fix all critical issues and improve test coverage"
/plan "refactor backupper service to follow hexagonal architecture"
/plan "add integration tests for archive functionality"
```

## Output Format
```
## Plan: [user_requirements]

### Tasks
1. [Priority] [Task description] - [Estimated effort]
2. [Priority] [Task description] - [Estimated effort]

### Dependencies
- Task 2 depends on Task 1
- Task 3 can run in parallel with Task 1

### Execution Order
1. Task 1
2. Task 2, Task 3 (parallel)
3. Task 4

### Files to Modify
- path/to/file1.go
- path/to/file2_test.go
```

## Plan Types
- `fix` - Address specific issues from review
- `refactor` - Improve code structure/architecture
- `enhance` - Add new functionality
- `optimize` - Performance improvements
- `test` - Add/improve test coverage