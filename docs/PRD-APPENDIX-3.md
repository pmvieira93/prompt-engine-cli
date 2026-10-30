# PRD Appendix 3: Installing from a Release

**Extends:** [PRD.md](../PRD.md) version 1.2.3 (section 6, target technology stack, and the roadmap)  
**Appendix Version:** 1.0.0  
**Date:** 2026-10-30  
**Status:** Proposed, deferred. Agreed as the direction on 2026-10-30, to be built later.

---

## 1. Background

Since v0.1.0, each tag publishes a release with a ready-built binary for macOS and Linux (arm64 and amd64) and `checksums.txt` (`.github/workflows/release.yml`). Installing still means one of two things:

* **`scripts/install.sh`**, which builds from source, so it needs Go and a clone of the repository; or
* **by hand**, following the README's "From a release" steps: download, extract, move the binary, and clear the macOS quarantine flag.

The script already does everything else a new user needs:
* the system check;
* finding the AI coding CLIs;
* installing missing tools with consent;
* the models;
* the config file;
* the health check.

Only getting the binary is tied to building it.

---

## 2. Proposal (FR-9)

One install script, with two ways to get the binary. A second script would repeat about 90% of the first and drift from it.

* **FR-9.1: Two sources.** `scripts/install.sh --from release | source`:
  * `release` is the default when the script isn't run from a clone, as in the one-line install;
  * `source` stays the default inside a clone, so building from a checkout works as today;
  * Go is needed only for `source`.
* **FR-9.2: Release install.** In order:
  1. detect the OS and architecture (darwin or linux, arm64 or amd64);
  2. download that platform's archive and `checksums.txt`, and stop if the checksum doesn't match;
  3. install `prompt-engine` to `~/.local/bin` (or `--prefix`);
  4. on macOS, clear the quarantine flag on that one binary;
  5. use the archive's `config.example.yaml` for a new config file.
* **FR-9.3: Versions.** The latest release by default; `--version vX.Y.Z` pins one.
* **FR-9.4: One-line install.**

  ```sh
  curl -fsSL https://raw.githubusercontent.com/pmvieira93/prompt-engine-cli/master/scripts/install.sh | bash
  ```

  The README also shows the URL pinned to a tag, for a reproducible install. The script already asks its questions on the terminal (`/dev/tty`) when it's piped in, so consent still works.
* **FR-9.5: Upgrades.** Running the script again:
  * replaces the binary;
  * keeps the config file;
  * shows the installed and latest versions (from `prompt-engine --pe-version`);
  * runs the health check.
* **FR-9.6 (optional): Checked releases.** After publishing, the release workflow installs the new release with the script, on Linux and macOS, and runs `prompt-engine --pe-version`. A release that can't be installed then fails visibly.

### 2.1 Documentation

* **README, Installation:**
  1. the one-line install (recommended);
  2. from a clone, with the same script;
  3. manually from a release;
  4. building by hand.
* **README, Requirements:** Go only to build from source.
* **Getting Started, step 1:** the one-line install, with the clone as the alternative.
* **Testing guide:** a new part, covering:
  * a release install;
  * a pinned version;
  * a bad checksum (the install stops);
  * an upgrade over an existing install, keeping the config.

---

## 3. Risks and mitigations

* **Trusting a piped script.** `curl … | bash` runs whatever the URL serves. The checksum protects the binary, not the script. Mitigations:
  * show the tag-pinned URL alongside the `master` one;
  * the script stays readable and asks before every change.
* **Unsigned macOS binary.** Clearing the quarantine flag works on most Macs, but company-managed ones may still block it. Signing and notarizing need an Apple Developer account: a separate decision, not part of this appendix.
* **Linux coverage.** The release path can be tested end to end on macOS against v0.1.0. On Linux it is tested only by FR-9.6.

---

## 4. Open decisions

* **FR-9.6:** whether to add the post-release install check, and on which runners.
* **The one-line URL:** whether the README leads with `master`, or with the latest tag (updated each release).

---

## 5. Roadmap

Deferred on 2026-10-30. When picked up: FR-9.1 to FR-9.5 with the documentation and the testing part, tested against a published release, then FR-9.6 if agreed.
