# Documax

Documax packages a directory tree into a portable text document and recreates
it later. Documents can be reviewed, pasted, validated, repaired, minimized,
and expanded using one Go CLI.

## Install from source

Requires Go 1.27 or later.

```sh
git clone https://github.com/sbanik/documax.git
cd documax
go test ./...
go build -o documax .
# optional:
go install .
```

## Document format

An expanded document has directory sections with relative file sections:

```text
|<--- DIRECTORY="project/" --->|
|<--- FILE="src/main.py" --->|
def greet():
    print("hello")
|>--- FILE ---<|
|<--- FILE="pyproject.toml" --->|
[project]
name = "example"
|>--- FILE ---<|
|>--- DIRECTORY ---<|
```

- Directory markers are `|<--- DIRECTORY="path/" --->|` and
  `|>--- DIRECTORY ---<|`.
- File markers are `|<--- FILE="relative/path" --->|` and
  `|>--- FILE ---<|`.
- Structural markers must not be indented.
- Empty directory sections are valid and are created when unpacking.
- File paths are relative to their directory section.

### Escaped marker content

If expanded file content contains Documax marker text, Documax escapes it and
marks the header with `;ESCAPED;`. The original content is restored at
unpack time.

```text
|<--- FILE="example.txt";ESCAPED; --->|
\|\<--- FILE="not-a-real-tag" --->|
|>--- FILE ---<|
```

### Minimized documents

`minimize` writes each file body in Base64 and marks it with the agreed
metadata:

```text
|<--- DIRECTORY="project/" --->||<--- FILE="src/main.py";ENCODED="BASE64"; --->|ZGVmIGdyZWV0KCk6CiAgICBwcmludCgiaGVsbG8iKQo=|>--- FILE ---<||>--- DIRECTORY ---<|
```

`expand` decodes Base64 and restores readable source. Base64 round-trips
indentation, blank lines, and other file bytes, which is especially useful for
Python files.

## Commands

### Pack a directory

```sh
documax pack --dir ./my-project --output project.documax.txt
```

Documax reads root-level `.gitignore` rules plus additional
`.documax.ignore` rules. The latter is never packed; `.gitignore` is
included. Empty directories are emitted as empty directory sections.

Packing has two phases: discovery calculates totals without publishing output,
then writing creates a temporary document and atomically publishes it only when
complete.

### Pack pasted content

```sh
documax pack --from-clipboard --dir project/ --output project.documax.txt
```

Enter a relative file path, paste its content, then finish with this exact line:

```text
|>--- CONTENT ---<|
```

Blank lines remain part of the pasted content. An empty file-path prompt ends
the session; an empty content block skips the pending file.

### Validate

```sh
documax validate project.documax.txt
```

Expanded and minimized documents are both valid inputs. Diagnostics are
editor-clickable:

```text
project.documax.txt:43: missing FILE closing tag before DIRECTORY closing tag
```

### Fix

```sh
documax fix project.documax.txt > fixed.documax.txt
documax fix --in-place project.documax.txt
```

Fixing is deliberately conservative. It removes structural-tag indentation,
adds a missing file closing tag before a directory transition, and appends
obvious missing closing tags at EOF. It does not attempt unsafe guesses about
paths or file contents.

### Minimize and expand

```sh
documax minimize project.documax.txt --output project.min.txt
documax expand project.min.txt --output project.expanded.txt
```

Use expanded documents for review and editing; use minimized documents for
compact transport.

### Unpack

```sh
documax unpack project.documax.txt --dir ./restored
```

Without an input argument, Documax uses `documax-output.txt` in the current
directory.

Unpack first validates the entire document. If invalid, it prints diagnostics,
attempts safe repairs in memory, validates the result again, then builds a
complete plan before changing the destination. Use `--fix-in-place` to save
automatic repairs to the source document.

Each destination file is written to a temporary sibling and atomically renamed
only after a complete write. Ctrl+C removes the active temporary file while
leaving completed files intact.

### Partial unpack

```sh
documax unpack project.documax.txt --dir ./restored --subpath d/e
```

`--subpath` matches consecutive directory components. For example `d/e`
matches both `a/b/d/e` and `a/b/c/d/e`.

### Absolute paths

The target directory may always be absolute:

```sh
documax unpack project.documax.txt --dir /Users/me/Projects
```

Embedded document paths are normally required to be relative and may not use
traversal. To trust and allow absolute paths embedded in a document:

```sh
documax unpack project.documax.txt --allow-absolute-paths
```

## Development

```sh
gofmt -w main.go main_test.go
go test ./...
go run . validate documax-output.txt
```

## Publish with Homebrew

Documax is a command-line application, so publish it as a Homebrew **formula**,
not a cask. The usual route is a personal tap named, for example,
`github.com/YOUR_GITHUB_USERNAME/homebrew-tap`.

1. Publish this repository and tag a release, such as `v0.1.0`.
2. Create a tap and formula:

   ```sh
   brew tap-new YOUR_GITHUB_USERNAME/homebrew-tap
   brew create \
     https://github.com/YOUR_GITHUB_USERNAME/documax/archive/refs/tags/v0.1.0.tar.gz \
     --tap YOUR_GITHUB_USERNAME/homebrew-tap \
     --set-name documax
   ```

3. Put this formula in the tap's `Formula/documax.rb`, replacing every
   placeholder:

   ```ruby
   class Documax < Formula
     desc "Package and restore directory trees as portable text documents"
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
         |<--- DIRECTORY="project/" --->|
         |<--- FILE="main.py" --->|
         print("hello")
         |>--- FILE ---<|
         |>--- DIRECTORY ---<|
       EOS
       system bin/"documax", "validate", "input.txt"
     end
   end
   ```

4. Calculate the release-tarball checksum:

   ```sh
   curl -L https://github.com/YOUR_GITHUB_USERNAME/documax/archive/refs/tags/v0.1.0.tar.gz \
     | shasum -a 256
   ```

5. Test, audit, commit, and push the tap:

   ```sh
   brew install --build-from-source YOUR_GITHUB_USERNAME/homebrew-tap/documax
   brew test YOUR_GITHUB_USERNAME/homebrew-tap/documax
   brew audit --strict --online YOUR_GITHUB_USERNAME/homebrew-tap/documax
   brew install YOUR_GITHUB_USERNAME/homebrew-tap/documax
   ```

Homebrew's current guidance for taps and formulae is available in the
[tap documentation](https://docs.brew.sh/How-to-Create-and-Maintain-a-Tap) and
[Formula Cookbook](https://docs.brew.sh/Formula-Cookbook).

## License

See [LICENSE](LICENSE).
