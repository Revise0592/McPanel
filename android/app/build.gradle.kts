plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
}

// Release builds are signed only when CI (or you) provides a key through
// these environment variables. Without them the release APK is unsigned,
// which is what F-Droid wants: it builds from source and signs it itself.
val releaseKeystore: String? = System.getenv("MCPANEL_KEYSTORE_FILE")

android {
    namespace = "io.github.revise0592.mcpanel"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.github.revise0592.mcpanel"
        minSdk = 26
        targetSdk = 36
        // Bump both for every release; F-Droid reads them from here.
        versionCode = 1
        versionName = "0.1.0"
    }

    signingConfigs {
        if (releaseKeystore != null) {
            create("release") {
                storeFile = file(releaseKeystore)
                storePassword = System.getenv("MCPANEL_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("MCPANEL_KEY_ALIAS")
                keyPassword = System.getenv("MCPANEL_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        debug {
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-debug"
        }
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            if (releaseKeystore != null) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    // Google's dependency metadata block is encrypted with Google's key, so
    // nobody else can read it. F-Droid rejects APKs that include it.
    dependenciesInfo {
        includeInApk = false
        includeInBundle = false
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    lint {
        warningsAsErrors = false
        abortOnError = true
    }
}

kotlin {
    jvmToolchain(17)
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.kotlinx.coroutines.android)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.material.icons.core)
    debugImplementation(libs.androidx.compose.ui.tooling)

    testImplementation(libs.junit)
    // Android's own org.json is only a stub in unit tests.
    testImplementation(libs.json)
}
