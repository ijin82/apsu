import java.util.Properties
import java.io.FileInputStream

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
}

val localProperties = Properties().apply {
    val localFile = rootProject.file("local.properties")
    if (localFile.exists()) {
        FileInputStream(localFile).use { load(it) }
    }
}

android {
    namespace = "io.github.ijin.apsu"
    compileSdk = 35

    val appVersion = project.findProperty("appVersion") as? String ?: "1.4.1"

    defaultConfig {
        applicationId = "io.github.ijin.apsu"
        minSdk = 26
        targetSdk = 34
        versionCode = 1
        versionName = appVersion

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    val storeFilePath = localProperties.getProperty("RELEASE_STORE_FILE")
    val storePassword = localProperties.getProperty("RELEASE_STORE_PASSWORD")
    val keyAlias = localProperties.getProperty("RELEASE_KEY_ALIAS")
    val keyPassword = localProperties.getProperty("RELEASE_KEY_PASSWORD")

    val hasReleaseSigning = !storeFilePath.isNullOrEmpty() &&
            file(storeFilePath).exists() &&
            !storePassword.isNullOrEmpty() &&
            !keyAlias.isNullOrEmpty()

    signingConfigs {
        if (hasReleaseSigning) {
            create("release") {
                storeFile = file(storeFilePath)
                this.storePassword = storePassword
                this.keyAlias = keyAlias
                this.keyPassword = keyPassword ?: storePassword
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            if (hasReleaseSigning) {
                signingConfig = signingConfigs.getByName("release")
            }
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
}

dependencies {
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.graphics)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    // Material icons core (minimal base set of icons)
    implementation("androidx.compose.material:material-icons-core")

    debugImplementation(libs.androidx.compose.ui.tooling)
}