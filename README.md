# Documax

Documax turns a directory tree into one portable document, then validates,
repairs, minimizes, expands, or unpacks it later. It is a Go CLI designed for
source code, configuration, Markdown, and other text files where whitespace
and indentation matter.

> Developer-only validation is provided by the separate `documax-dev` executable and is not included in the distributed `documax` binary.

## Build

Requires Go 1.27 or later.

```sh
git clone https://github.com/YOUR_GITHUB_USERNAME/documax.git
cd documax
go test ./...
go build -o documax ./cmd/documax
```

## Formats

Bracket syntax is the default:

```text
[DIR: project/]
[FILE: src/main.py]
def greet():
    print("hello")
[/FILE]
[FILE: pyproject.toml]
[project]
name = "example"
[/FILE]
[/DIR]
```

The XML syntax is available when packing with `--format xml`:

```xml
<d:dir path="project/">
<d:file path="src/main.py">
def greet():
    print("hello")
</d:file>
</d:dir>
```

Documax automatically detects bracket and XML documents for `validate`,
`fix`, `minimize`, `expand`, and `unpack`.

- File paths are relative to their directory section.
- Structural tags must not be indented.
- Empty directory sections are valid and are created by unpacking.
- During packing, a payload containing structural tags is automatically stored
  as `ENC=GZ+B64` so it cannot be mistaken for document structure. `unpack`
  restores its original bytes.

## Minimized documents

`minimize` compresses every raw file payload using Gzip and encodes the
compressed bytes in standard Base64. The bracket metadata is:

```text
[FILE: src/main.py;ENC=GZ+B64]H4sIAAAAAAAA...[/FILE]
```

The XML equivalent is:

```xml
<d:file path="src/main.py" encoding="gzip+base64">H4sIAAAAAAAA...</d:file>
```

`expand` and `unpack` reverse the encoding. Since compression and Base64
operate on bytes, Python indentation, YAML spacing, blank lines, and final
newlines survive a minimize/expand/unpack round-trip.

`minimize` and `expand` overwrite their input document by default. Use
`-o` or `--output` to write the transformed document to a separate file.

## Commands

### Pack

```sh
documax pack
documax pack --format xml
documax pack --dir ./my-project
documax pack --dir ./my-project --output ./archives/
documax pack --dir ./my-project --output ./archives/project.txt
```

Without flags, `pack` packages the current directory as `documax-output.md`
(`documax-output.xml` for `--format xml`). With `--dir` and no output path,
the output is `<selected-directory>-documax.md` or `.xml` in the current
directory; spaces in the selected directory name become dashes. An output
path with no extension is treated as a directory and receives
`<selected-directory>/documax-output.md` or `.xml`. An output path ending in
`.md`, `.xml`, or `.txt` is used as the exact filename. A non-empty existing
output file requires an overwrite confirmation.

Root-level `.gitignore` patterns are applied along with additional
`.documax.ignore` patterns. `.documax.ignore` is excluded; `.gitignore`
is retained. Packing also always excludes common repository, operating-system,
IDE, cache, and build output paths such as `.git`, `.DS_Store`, `.idea`,
`.vscode`, `__pycache__`, `node_modules`, `target`, `build`, and
`dist`. Packing first discovers eligible directories/files, then writes a
temporary output and atomically publishes it on success.

Use `-m` or `--minimized` to write GZ+B64 output directly, without running a
separate minimize command.

### Pack pasted content

```sh
documax pack --from-clipboard --dir project/ --output project.txt
```

Enter a relative path, paste content, then enter `CONTENT` on its
own line. Blank lines are preserved. An empty path ends the session; empty
content skips that pending file.

### Validate and fix

```sh
documax validate project.documax.txt
documax fix project.documax.txt > fixed.txt
documax fix --in-place project.documax.txt
```

Diagnostics use editor-clickable locations:

```text
project.documax.txt:43: missing FILE closing tag before DIRECTORY closing tag
```

`fix` only removes structural-tag indentation and inserts obvious missing
closing tags before directory transitions or at EOF.

### Minimize and expand

```sh
documax minimize project.txt --output project.min.txt
documax expand project.min.txt --output project.expanded.txt
documax minimize
documax expand
```

Minification preserves the input document's bracket or XML syntax.
Without a filepath, each command looks for exactly one of
`documax-output.md` and `documax-output.xml` in the current directory. It
errors if neither—or both—exist.

### Unpack

```sh
documax unpack project.txt --dir ./restored
documax unpack project.txt --dir ./restored --subpath d/e
documax unpack project.min.txt --dir ./restored
```

If no input filename is supplied, Documax looks for exactly one of
`documax-output.md` and `documax-output.xml` in the current directory. It
errors if neither—or both—exist. `--subpath c` matches any directory component
named `c`; `--subpath d/e` matches that consecutive component sequence.
Minimized GZ+B64 documents unpack directly; an explicit expand step is not
required.

Unpack validates first. If validation fails it prints diagnostics, tries safe
in-memory repairs, validates again, then creates a complete phase-one plan.
Phase two creates directories and atomically writes each file. Use
`--fix-in-place` to save automatic repairs to the source document.

The output directory itself may be absolute. Embedded document paths are
normally restricted to safe relative paths. Use `--allow-absolute-paths`
only for trusted documents that intentionally contain absolute paths.

### Verify a pack/unpack round-trip

This is a development-only command and is excluded from normal/Homebrew
builds. Run it with the separate developer executable:

~~~sh
go run ./cmd/documax-dev validate-pack-unpack --dir ./my-project
~~~

It creates a temporary archive and a temporary sibling unpack directory, then
compares every directory and file that packing would include. It uses the same
default exclusions and ignore files as pack, and compares file content
byte-for-byte. Add --keep-artifacts to retain the archive and unpacked
directory for inspection.

## Homebrew

Publish Documax as a Homebrew **formula**, not a cask. After creating a tagged
GitHub release, create a personal tap and add `Formula/documax.rb`:

```ruby
class Documax < Formula
  desc "Package and restore directory trees as portable documents"
  homepage "https://github.com/YOUR_GITHUB_USERNAME/documax"
  url "https://github.com/YOUR_GITHUB_USERNAME/documax/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "REPLACE_WITH_RELEASE_TARBALL_SHA256"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(output: bin/"documax"), "./cmd/documax"
  end

  test do
    (testpath/"input.txt").write <<~EOS
      [DIR: project/]
      [FILE: main.py]
      print("hello")
      [/FILE]
      [/DIR]
    EOS
    system bin/"documax", "validate", "input.txt"
  end
end
```

```sh
brew tap-new YOUR_GITHUB_USERNAME/homebrew-tap
brew install --build-from-source YOUR_GITHUB_USERNAME/homebrew-tap/documax
brew test YOUR_GITHUB_USERNAME/homebrew-tap/documax
brew audit --strict --online YOUR_GITHUB_USERNAME/homebrew-tap/documax
```

See the official [tap guide](https://docs.brew.sh/How-to-Create-and-Maintain-a-Tap)
and [Formula Cookbook](https://docs.brew.sh/Formula-Cookbook).

## Development

```sh
gofmt -w cmd/documax/*.go cmd/documax-dev/*.go internal/core/*.go internal/documax/*.go internal/devtools/*.go
go test ./...
```

## License

See [LICENSE](LICENSE).
