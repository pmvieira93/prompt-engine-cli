# PRD Appendix 3: Installing from a Release

**Extends:** [PRD.md](../PRD.md) version 1.2.3 (section 6, target technology stack, and the roadmap)  
**Appendix Version:** 1.2.0  
**Date:** 2026-11-02  
**Status:** FR-9.1 to FR-9.6 built on 2026-11-02 (see section 6). FR-9.7 (signed macOS releases), added the same day, is proposed and needs an Apple Developer account first.

---

## 1. Background

Since v0.1.0, each tag publishes a release with a ready-built binary for macOS and Linux (arm64 and amd64) and `checksums.txt` (`.github/workflows/release.yml`). Installing still means one of two things:

* **`install.sh`** (at the repository root since v0.1.2), which builds from source, so it needs Go and a clone of the repository; or
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

* **FR-9.1: Two sources.** `install.sh --from release | source`:
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
  curl -fsSL https://raw.githubusercontent.com/pmvieira93/prompt-engine-cli/master/install.sh | bash
  ```

  The README also shows the URL pinned to a tag, for a reproducible install. The script already asks its questions on the terminal (`/dev/tty`) when it's piped in, so consent still works.
* **FR-9.5: Upgrades.** Running the script again:
  * replaces the binary;
  * keeps the config file;
  * shows the installed and latest versions (from `prompt-engine --pe-version`);
  * runs the health check.
* **FR-9.7: Signed and notarized macOS releases.**
  * *Seen on 2026-11-02:* on a company Mac, the v0.1.2 binary downloaded with a browser was blocked at its first run: *"prompt-engine" Not Opened. Apple could not verify "prompt-engine" is free of malware that may harm your Mac or compromise your privacy.* Clearing the quarantine flag (`xattr -d com.apple.quarantine`) or "Open Anyway" in System Settings works around it, but only where the Mac allows it, and it asks every user to trust an unknown binary.
  * **Signing.** In the release workflow, on a macOS runner, each darwin binary is signed with a *Developer ID Application* certificate, with the hardened runtime and a secure timestamp (`codesign --options runtime --timestamp`), before it is archived.
  * **Notarizing.** The signed binaries are sent to Apple's notary service (`xcrun notarytool submit --wait`) and the release only goes ahead once they are accepted. A bare executable can't carry a stapled ticket, so Gatekeeper checks the notarization online the first time it runs.
  * **Secrets.** The certificate (as a `.p12`), its password, and an App Store Connect API key for the notary service are kept as GitHub Actions secrets. They are imported into a temporary keychain for the job and deleted after it.
  * **The install script.** With signed releases, FR-9.2 no longer needs to clear the quarantine flag. It keeps doing so only for unsigned builds.
  * **Linux** has no equivalent check. Signing `checksums.txt` (with GPG or Sigstore's cosign) would let users verify where a release came from; that's optional and separate.
  * **Checked:** the signed binary passes `codesign --verify --strict` and `spctl --assess --type execute`, and runs on a Mac that never ran it before, downloaded with a browser, without the dialog.
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
* **Unsigned macOS binary.** Until FR-9.7, clearing the quarantine flag works on most Macs, but company-managed ones may still block it.
* **Signing secrets (FR-9.7).** Whoever holds the certificate can sign software in the account's name. Mitigations:
  * the secrets are only readable by the release workflow, which only runs on tags;
  * the temporary keychain is deleted after each job;
  * the certificate can be revoked from the Apple Developer account if it leaks.
* **Hardened runtime (FR-9.7).** Notarization needs it, and it can block some things a program does. `prompt-engine` opens a pseudo-terminal, starts `claude` and talks to Ollama on localhost, which shouldn't need special entitlements. The first signed build checks that it still works.
* **Company security software.** Signing makes it much less likely, but endpoint security tools may still flag a program that reads every keystroke and sits between the user and another program. That is a decision for each company's IT, which signing makes easier to approve.
* **Linux coverage.** The release path can be tested end to end on macOS against v0.1.0. On Linux it is tested only by FR-9.6.

---

## 4. Open decisions

* ~~**FR-9.6:** whether to add the post-release install check, and on which runners.~~ Added, on Linux and macOS (section 6).
* ~~**The one-line URL:** whether the README leads with `master`, or with the latest tag (updated each release).~~ The README leads with `master`, which always installs the latest release, and shows a tag-pinned line beside it.
* **FR-9.7, the Apple Developer account:** a paid, yearly Apple Developer Program membership. Individual or organization? The name on the certificate is what macOS shows as the developer.
* **FR-9.7, Linux:** whether to sign `checksums.txt` as well.

---

## 5. Roadmap

Deferred on 2026-10-30; FR-9.1 to FR-9.6 built on 2026-11-02 (section 6). The one-line install works from `master` once this is merged; the release-install check (FR-9.6) first runs with the next tag.

FR-9.7 doesn't depend on the others. It can be done on its own, as soon as there's an Apple Developer account: the signing and notarizing steps in the release workflow, the secrets, and a check on a fresh Mac.

---

## 6. As built (2026-11-02)

FR-9.1 to FR-9.6 as proposed, with these details:

* **The latest release** comes from where `<releases>/latest` redirects to, not from GitHub's API, so it has no rate limit.
* **Same version:** when the installed binary already reports the release's version (`--pe-version`), nothing is downloaded. The one exception is a missing config file: the archive is downloaded again for its template.
* **An older binary:** one that can't report its version (built before `--pe-version`) is simply replaced.
* **`--version` implies `--from release`,** even in a clone; with `--from source` it's an error.
* **The binary is replaced in one step:** the new one is copied next to the old one, then moved over it. A running `prompt-engine` keeps working, and a failed install leaves the old binary.
* **`--binary-only`:** installs or upgrades only the binary, with no dependencies, models or health check. It's the quickest upgrade, and what the tests use.
* **Source builds report a version:** `--from source` builds with `git describe` as the version (e.g. `v0.1.2-5-g343ef94`), so an upgrade can show both versions.
* **Hints:** without a clone, the script's hints show the one-line command (`… | bash -s -- --check`) in place of `./install.sh`.
* **`PROMPT_ENGINE_RELEASES_URL`** replaces `https://github.com/pmvieira93/prompt-engine-cli/releases`, for tests and mirrors (`file://` works).
* **Tests:**
  * `scripts/install-test.sh` builds releases on the spot and serves them from a folder. It covers a pinned version, the same version again, an upgrade that keeps the config, a bad checksum, a missing config file, an old binary without `--pe-version`, the script piped in outside a clone, and a missing version. `--latest` adds the real latest release.
  * CI runs it on Linux and macOS on every push. The release workflow then installs each new release on both (FR-9.6).
  * The testing guide's Part P checks the same by hand against GitHub.
* **Checked on macOS:** a binary downloaded with `curl` has no quarantine flag. On Linux, only CI checks it.

