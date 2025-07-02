# Bump-In Feature

The `goversion` tool now supports bumping version numbers in arbitrary files beyond just Go source files. This is useful for keeping version numbers synchronized across multiple files in your project.

## Usage

Use the `-bump-in` flag to specify files where version numbers should be found and updated:

```bash
# Bump version in package.json
goversion -bump-in=package.json patch

# Bump version in multiple files
goversion -bump-in=package.json -bump-in=README.md -bump-in=extension.toml minor

# Combine with other flags
goversion -version-file=./pkg/version.go -bump-in=package.json -bump-in=Cargo.toml major
```

## Supported File Formats

The tool automatically detects and updates version patterns in various file formats:

### JSON Files (package.json, composer.json, etc.)
```json
{
  "name": "my-app",
  "version": "1.2.3"
}
```

### TOML Files (extension.toml, Cargo.toml, pyproject.toml, etc.)
```toml
[package]
name = "my-extension"
version = "1.2.3"
```

### YAML Files
```yaml
app:
  name: MyApp
  version: "1.2.3"
```

### README and Documentation Files
```markdown
# My Project

Current version: v1.2.3

## Installation

Install version 1.2.3 with: npm install my-project@1.2.3
```

### XML Files
```xml
<project>
  <version>1.2.3</version>
</project>
```

### Python setup.py
```python
setup(
    name="mypackage",
    version="1.2.3",
)
```

### Generic VERSION Files
```
VERSION=1.2.3
```

## Version Patterns

The tool recognizes various version patterns:

- `version: "1.2.3"` - YAML/JSON style
- `version = "1.2.3"` - TOML/Python style
- `VERSION=1.2.3` - Environment variable style
- `"version": "1.2.3"` - JSON object style
- `<version>1.2.3</version>` - XML style
- `@version 1.2.3` - Documentation comment style
- `Current version: v1.2.3` - Natural language
- `Install version 1.2.3` - Installation instructions

## Behavior

1. **Version Format Preservation**: The tool preserves the format of version strings. If a file uses `v1.2.3` (with v prefix), it will be updated to `v1.3.0`. If it uses `1.2.3` (without v prefix), it will be updated to `1.3.0`.

2. **Multiple Versions**: If a file contains multiple version references, all of them will be updated.

3. **Atomic Updates**: All files are updated together and included in the same git commit along with the main version file.

4. **Dry Run Support**: The `-dry` flag works with bump-in files, showing which files would be modified without actually changing them.

## Examples

### Update version in a VS Code extension
```bash
goversion -bump-in=extension.toml -bump-in=package.json patch
```

### Update version in a Rust project
```bash
goversion -bump-in=Cargo.toml minor
```

### Update version in Python project files
```bash
goversion -bump-in=setup.py -bump-in=pyproject.toml -bump-in=__init__.py major
```

### Update version across documentation
```bash
goversion -bump-in=README.md -bump-in=docs/installation.md -bump-in=CHANGELOG.md 2.0.0
```

## Integration with Existing Workflow

The bump-in feature integrates seamlessly with goversion's existing functionality:

- Files specified with `-bump-in` are automatically staged and committed
- The version bump follows the same rules as the main version file
- Git tags are created as usual
- Major version bumps still update go.mod and imports when applicable

## Error Handling

- If a bump-in file doesn't exist, an error is reported
- If a bump-in file contains no recognizable version patterns, it's silently skipped
- If a bump-in file cannot be written, the entire operation is aborted
- The working directory must be clean (except for files being bumped)
