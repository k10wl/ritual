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