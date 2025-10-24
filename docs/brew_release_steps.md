### Checklist for Releasing v0.1.1 (Example)

1.  **Code Changes:**
    * Complete and test the features/bugfixes for v0.1.1 (e.g., implement Option 2 for `stack start`, add PDF parser integration, fix bugs).
    * Ensure `cmd/aleutian/main.go`'s `version` variable is still set (GoReleaser uses this).

2.  **Merge to `main`:**
    * Ensure all relevant feature branches are merged into your `main` branch.
    * Pull the latest `main` locally (`git checkout main && git pull`).

3.  **Tag the Release:**
    * Create a new Git tag pointing to the final commit for the release on `main`:
        ```bash
        git tag v0.1.1
        ```
    * Push the tag to GitHub:
        ```bash
        git push origin v0.1.1
        ```

4.  **Generate Release Assets (GoReleaser):**
    * Run GoReleaser (from your local `AleutianLocal` repo root):
        ```bash
        # Ensure .goreleaser.yaml is up-to-date
        # Dry run (optional): goreleaser release --snapshot --clean
        goreleaser release --clean
        ```
    * Verify on GitHub: Check the "Releases" page for `v0.1.1`. Ensure binaries (`.tar.gz`, `.zip`) and `checksums.txt` are attached. Release notes should be generated.

5.  **Update Homebrew Formula:**
    * **Get New URL & Checksum:**
        * Go to the `v0.1.1` release page on GitHub.
        * Copy the link address for the "Source code (tar.gz)".
        * Download the `v0.1.1.tar.gz` file.
        * Calculate its SHA256 checksum: `shasum -a 256 AleutianLocal-0.1.1.tar.gz` (macOS) or `sha256sum ...` (Linux).
    * **Edit Formula File:**
        * Clone/pull your `homebrew-aleutian` tap repository locally.
        * Edit `Formula/aleutian.rb`.
        * Update the `url` line with the new tarball URL.
        * Update the `sha256` line with the new checksum.
    * **Commit & Push Formula Update:**
        ```bash
        # Inside homebrew-aleutian repo
        git add Formula/aleutian.rb
        git commit -m "Update aleutian formula to v0.1.1"
        git push origin main
        ```

6.  **(Optional) Verify Homebrew Update:**
    * Run `brew update` locally.
    * Run `brew upgrade aleutian` (or `brew reinstall aleutian`).
    * Check `aleutian --version` shows `0.1.1`.