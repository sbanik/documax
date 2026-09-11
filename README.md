# Documax

Documax turns a directory tree into one portable document, then validates,
repairs, minimizes, expands, or unpacks it later. It is a Go CLI designed for
source code, configuration, Markdown, and other text files where whitespace
and indentation matter.

## Build

Requires Go 1.27 or later.

```sh
git clone https://github.com/YOUR_GITHUB_USERNAME/documax.git
cd documax
go test ./...
go build -o documax .
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
- Do not use unescaped structural tags as ordinary expanded file content.

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

## Commands

### Pack

```sh
documax pack --dir ./my-project --output project.documax.txt
documax pack --dir ./my-project --format xml --output project.xml.txt
```

Root-level `.gitignore` patterns are applied along with additional
`.documax.ignore` patterns. `.documax.ignore` is excluded; `.gitignore`
is retained. Packing first discovers eligible directories/files, then writes a
temporary output and atomically publishes it on success.

### Pack pasted content

```sh
documax pack --from-clipboard --dir project/ --output project.txt
```

Enter a relative path, paste content, then enter `|>--- CONTENT ---<|` on its
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
```

Minification preserves the input document's bracket or XML syntax.

### Unpack

```sh
documax unpack project.txt --dir ./restored
documax unpack project.txt --dir ./restored --subpath d/e
```

If no input filename is supplied, Documax uses `documax-output.txt` in the
current directory. `--subpath c` matches any directory component named
`c`; `--subpath d/e` matches that consecutive component sequence.

Unpack validates first. If validation fails it prints diagnostics, tries safe
in-memory repairs, validates again, then creates a complete phase-one plan.
Phase two creates directories and atomically writes each file. Use
`--fix-in-place` to save automatic repairs to the source document.

The output directory itself may be absolute. Embedded document paths are
normally restricted to safe relative paths. Use `--allow-absolute-paths`
only for trusted documents that intentionally contain absolute paths.

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
    system "go", "build", *std_go_args(output: bin/"documax"), "."
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
gofmt -w main.go main_test.go
go test ./...
```

## License

See [LICENSE](LICENSE).
