# McPanel for Android

A companion app for McPanel. It pairs with your own panel and lets you
check on your servers from your phone. It talks to the panel's JSON API,
described in [`docs/mobile-api.md`](../docs/mobile-api.md).

This is an early version: so far it's only the project skeleton.

- App ID: `io.github.revise0592.mcpanel` (debug builds add `.debug`, so both
  can be installed side by side)
- Android 8.0 (API 26) and newer
- Kotlin and Jetpack Compose, with no Google Play services. This keeps it
  F-Droid friendly; keep it that way when adding libraries.

## Building

You need JDK 17 or newer and the Android SDK. Android Studio installs both.
Open the `android/` folder in Android Studio, or from a terminal:

```sh
cd android
./gradlew assembleDebug        # app/build/outputs/apk/debug/app-debug.apk
./gradlew lintDebug
```

Every push that changes `android/` is built by the
[Android workflow](../.github/workflows/android.yml). Each run has the
debug APK attached as an artifact, so you can install a build on your phone
without building it yourself.

## Releasing on GitHub

App releases use their own tags, `android-v0.1.0`, so they don't get mixed
up with panel releases (`v1.2.3`).

1. In `app/build.gradle.kts`, increase `versionCode` by one and set
   `versionName`.
2. Add `fastlane/metadata/android/en-US/changelogs/<versionCode>.txt` with
   what changed. It becomes the release notes, and F-Droid shows it too.
3. Commit, then tag and push:

   ```sh
   git tag android-v0.1.0
   git push origin android-v0.1.0
   ```

The workflow checks that the tag matches `versionName` and builds a signed
APK. It then publishes a GitHub release with the APK and its SHA-256. The
release isn't marked "latest", so the panel's own release stays the latest
one.

### Signing key (one-time setup)

Android only installs an update when it's signed with the same key as the
installed app, so **make this key once and never lose it**. Keep a copy of
the keystore and its passwords somewhere safe, outside the repo.

```sh
keytool -genkeypair -v -keystore mcpanel-release.jks -alias mcpanel \
  -keyalg RSA -keysize 4096 -validity 10000
base64 -w0 mcpanel-release.jks   # on macOS: base64 -i mcpanel-release.jks
```

Then add these repository secrets (Settings → Secrets and variables →
Actions):

| Secret | Value |
|---|---|
| `ANDROID_KEYSTORE_BASE64` | The base64 output above |
| `ANDROID_KEYSTORE_PASSWORD` | The keystore password |
| `ANDROID_KEY_ALIAS` | `mcpanel` |
| `ANDROID_KEY_PASSWORD` | The key password (the same as the keystore password unless you chose otherwise) |

## F-Droid

F-Droid builds the app from this repo itself, using the `android` folder,
and signs it with F-Droid's own key. Without the signing variables, a
release build here is unsigned, which is what F-Droid expects. Its listing
text comes from `fastlane/metadata/android/en-US/`.

Because the keys differ, an APK from GitHub can't be updated with the
F-Droid version (or the other way round) without uninstalling first. F-Droid
can publish the GitHub-signed APK instead if the build is reproducible; that
can be set up later in the F-Droid metadata.
