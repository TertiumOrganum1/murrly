# Murrly Homebrew tap

This directory contains the Homebrew formula for distributing Murrly via
`brew install`. To publish:

1. Create a public GitHub repo named **`homebrew-murrly`** under
   `github.com/tertiumorganum1/`. The `homebrew-` prefix is mandatory —
   `brew` strips it when resolving `tertiumorganum1/murrly/<formula>`.

2. Copy `murrly.rb` from this folder to `Formula/murrly.rb` in that
   tap repo.

3. Tag a Murrly release in the main repo:

   ```bash
   git tag v0.2.0
   git push --tags
   ```

4. Compute the tarball SHA-256:

   ```bash
   curl -sL https://github.com/tertiumorganum1/murrly/archive/refs/tags/v0.2.0.tar.gz | shasum -a 256
   ```

5. Replace `REPLACE_WITH_TARBALL_SHA256` in `Formula/murrly.rb` with the
   value from step 4.

6. Commit and push the tap repo. Users can now install:

   ```bash
   brew install tertiumorganum1/murrly/murrly
   ```

## What the formula does

- Builds whisper.cpp from source (Metal on macOS, CUDA-or-CPU on Linux).
- Builds the Murrly Go binary.
- macOS: assembles `/Applications/Murrly.app` via `scripts/install-mac.sh`.
- Linux: installs a `.desktop` launcher entry and the colored cat app
  icon to the standard `share/applications/` + `share/icons/hicolor/`.

## On each new release

1. Update the `url` and `version` in `Formula/murrly.rb`.
2. Recompute and replace the SHA-256.
3. Commit and push the tap.

Users get the new version with `brew upgrade murrly`.
