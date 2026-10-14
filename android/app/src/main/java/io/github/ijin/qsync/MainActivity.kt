package io.github.ijin.qsync

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.provider.DocumentsContract
import android.provider.Settings
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.net.Inet4Address
import java.net.NetworkInterface

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.ui.res.painterResource
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll

enum class AppThemeMode {
    SYSTEM, LIGHT, DARK
}

private val DarkColors = darkColorScheme(
    primary = Color(0xFF9FA8DA),
    secondary = Color(0xFF64B5F6),
    background = Color(0xFF121418),
    surface = Color(0xFF1E2228),
    surfaceVariant = Color(0xFF282D36),
    onPrimary = Color(0xFF121418),
    onSecondary = Color(0xFF121418),
    onBackground = Color(0xFFE2E2E6),
    onSurface = Color(0xFFE2E2E6),
    onSurfaceVariant = Color(0xFFC4C7D0)
)

private val LightColors = lightColorScheme(
    primary = Color(0xFF283593),
    secondary = Color(0xFF1E88E5),
    background = Color(0xFFF8F9FA),
    surface = Color(0xFFFFFFFF),
    surfaceVariant = Color(0xFFF1F3F5),
    onPrimary = Color.White,
    onSecondary = Color.White,
    onBackground = Color(0xFF1F1F1F),
    onSurface = Color(0xFF1F1F1F),
    onSurfaceVariant = Color(0xFF49454F)
)

class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        ensureDefaultDownloadFolder()

        val prefs = getSharedPreferences("qsync_prefs", Context.MODE_PRIVATE)

        setContent {
            val themeModeStr = remember {
                mutableStateOf(prefs.getString("theme_mode", AppThemeMode.SYSTEM.name) ?: AppThemeMode.SYSTEM.name)
            }
            val themeMode = try {
                AppThemeMode.valueOf(themeModeStr.value)
            } catch (e: Exception) {
                AppThemeMode.SYSTEM
            }

            val isDark = when (themeMode) {
                AppThemeMode.SYSTEM -> isSystemInDarkTheme()
                AppThemeMode.LIGHT -> false
                AppThemeMode.DARK -> true
            }

            MaterialTheme(
                colorScheme = if (isDark) DarkColors else LightColors
            ) {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = MaterialTheme.colorScheme.background
                ) {
                    QSyncMainScreen(
                        onRequestAllFilesPermission = { requestAllFilesAccess() },
                        onStartServer = { QSyncService.start(this) },
                        onStopServer = { QSyncService.stop(this) },
                        getWifiIp = { getDeviceIpAddress() },
                        configFile = File(filesDir, "qsync_config.json"),
                        currentThemeMode = themeMode,
                        onThemeModeChanged = { newMode ->
                            prefs.edit().putString("theme_mode", newMode.name).apply()
                            themeModeStr.value = newMode.name
                        }
                    )
                }
            }
        }
    }

    private fun requestAllFilesAccess() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            try {
                val intent = Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION).apply {
                    data = Uri.parse("package:$packageName")
                }
                startActivity(intent)
            } catch (e: Exception) {
                val intent = Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION)
                startActivity(intent)
            }
        }
    }

    private fun ensureDefaultDownloadFolder() {
        val configFile = File(filesDir, "qsync_config.json")
        if (!configFile.exists()) {
            val downloadDir = Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS)
            val downloadPath = downloadDir.absolutePath
            val defaultJson = """
            {
                "server_name": "${Build.MODEL}",
                "port": 8484,
                "folders": [
                    {
                        "id": "downloads",
                        "name": "Download",
                        "path": "$downloadPath"
                    }
                ],
                "trusted_devices": {}
            }
            """.trimIndent()
            configFile.writeText(defaultJson)
        }
    }

    private fun getDeviceIpAddress(): String {
        try {
            val interfaces = NetworkInterface.getNetworkInterfaces()
            while (interfaces.hasMoreElements()) {
                val networkInterface = interfaces.nextElement()
                val addresses = networkInterface.inetAddresses
                while (addresses.hasMoreElements()) {
                    val address = addresses.nextElement()
                    if (!address.isLoopbackAddress && address is Inet4Address) {
                        return address.hostAddress ?: "127.0.0.1"
                    }
                }
            }
        } catch (e: Exception) {
            e.printStackTrace()
        }
        return "127.0.0.1"
    }
}

data class SharedFolder(val id: String, val name: String, val path: String)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun QSyncMainScreen(
    onRequestAllFilesPermission: () -> Unit,
    onStartServer: () -> Unit,
    onStopServer: () -> Unit,
    getWifiIp: () -> String,
    configFile: File,
    currentThemeMode: AppThemeMode,
    onThemeModeChanged: (AppThemeMode) -> Unit
) {
    val coroutineScope = rememberCoroutineScope()
    val httpClient = remember { OkHttpClient() }

    var isRunning by remember { mutableStateOf(QSyncService.isRunning) }
    var currentIp by remember { mutableStateOf(getWifiIp()) }
    var hasFullStoragePermission by remember {
        mutableStateOf(
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                Environment.isExternalStorageManager()
            } else true
        )
    }

    var pairPin by remember { mutableStateOf<String?>(null) }
    var folders by remember { mutableStateOf(listOf<SharedFolder>()) }

    // Settings modal and state
    var showSettingsDialog by remember { mutableStateOf(false) }
    var deviceName by remember { mutableStateOf(Build.MODEL) }

    // Add folder dialog state
    var showAddDialog by remember { mutableStateOf(false) }
    var newFolderName by remember { mutableStateOf("") }
    var newFolderPath by remember { mutableStateOf("") }

    // Helper to read config (folders and server_name)
    fun readConfig() {
        try {
            if (configFile.exists()) {
                val json = JSONObject(configFile.readText())
                val name = json.optString("server_name", Build.MODEL)
                if (name.isNotBlank()) {
                    deviceName = name
                }
                val fArray = json.optJSONArray("folders") ?: JSONArray()
                val list = mutableListOf<SharedFolder>()
                for (i in 0 until fArray.length()) {
                    val item = fArray.getJSONObject(i)
                    list.add(
                        SharedFolder(
                            id = item.optString("id", i.toString()),
                            name = item.optString("name", "Folder"),
                            path = item.optString("path", "")
                        )
                    )
                }
                folders = list
            }
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }

    // Helper to save folders directly to config file
    fun saveFoldersToConfig(newList: List<SharedFolder>) {
        try {
            val json = if (configFile.exists()) JSONObject(configFile.readText()) else JSONObject()
            val fArray = JSONArray()
            for (f in newList) {
                val item = JSONObject().apply {
                    put("id", f.id)
                    put("name", f.name)
                    put("path", f.path)
                }
                fArray.put(item)
            }
            json.put("folders", fArray)
            json.put("server_name", deviceName)
            configFile.writeText(json.toString(2))
            folders = newList
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }

    // Helper to save device name
    fun saveDeviceNameToConfig(newName: String) {
        try {
            val json = if (configFile.exists()) JSONObject(configFile.readText()) else JSONObject()
            json.put("server_name", newName)
            configFile.writeText(json.toString(2))
            deviceName = newName
        } catch (e: Exception) {
            e.printStackTrace()
        }
    }

    // Initial load
    LaunchedEffect(Unit) {
        readConfig()
    }

    // System folder picker launcher
    val folderPickerLauncher = rememberLauncherForActivityResult(
        contract = ActivityResultContracts.OpenDocumentTree()
    ) { uri: Uri? ->
        if (uri != null) {
            // Convert tree URI to filesystem path
            val docId = DocumentsContract.getTreeDocumentId(uri)
            val parts = docId.split(":")
            val path = if (parts.size > 1) {
                if (parts[0].equals("primary", ignoreCase = true)) {
                    "/storage/emulated/0/" + parts[1]
                } else {
                    "/storage/" + parts[0] + "/" + parts[1]
                }
            } else {
                "/storage/emulated/0"
            }
            newFolderPath = path
            val base = File(path).name
            if (newFolderName.isBlank()) {
                newFolderName = if (base.isEmpty() || base == "0") "Storage" else base
            }
            showAddDialog = true
        }
    }

    // Periodic status poll
    LaunchedEffect(Unit) {
        while (true) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                hasFullStoragePermission = Environment.isExternalStorageManager()
            }
            currentIp = getWifiIp()
            isRunning = QSyncService.isRunning
            delay(2000)
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("qSync", fontWeight = FontWeight.Bold) },
                navigationIcon = {
                    IconButton(onClick = { showSettingsDialog = true }) {
                        Icon(
                            imageVector = Icons.Default.Menu,
                            contentDescription = "Settings",
                            tint = Color.White
                        )
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = MaterialTheme.colorScheme.primary,
                    titleContentColor = Color.White
                )
            )
        }
    ) { padding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp)
        ) {
            // 1. Permission Warning Card if not granted
            if (!hasFullStoragePermission) {
                item {
                    Card(
                        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.errorContainer),
                        shape = RoundedCornerShape(12.dp)
                    ) {
                        Column(modifier = Modifier.padding(16.dp)) {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Icon(
                                    Icons.Default.Warning,
                                    contentDescription = null,
                                    tint = MaterialTheme.colorScheme.onErrorContainer
                                )
                                Spacer(modifier = Modifier.width(8.dp))
                                Text(
                                    "Storage Permission Needed",
                                    fontWeight = FontWeight.Bold,
                                    color = MaterialTheme.colorScheme.onErrorContainer
                                )
                            }
                            Spacer(modifier = Modifier.height(8.dp))
                            Text(
                                "To access files in folders like Downloads or Photos created by other apps, grant 'All files access'.",
                                fontSize = 14.sp,
                                color = MaterialTheme.colorScheme.onErrorContainer
                            )
                            Spacer(modifier = Modifier.height(12.dp))
                            Button(
                                onClick = onRequestAllFilesPermission,
                                colors = ButtonDefaults.buttonColors(
                                    containerColor = MaterialTheme.colorScheme.error,
                                    contentColor = MaterialTheme.colorScheme.onError
                                )
                            ) {
                                Text("Grant Permission in 1-Tap")
                            }
                        }
                    }
                }
            }

            // 2. Server Status Card
            item {
                Card(
                    shape = RoundedCornerShape(12.dp),
                    colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
                    elevation = CardDefaults.cardElevation(2.dp)
                ) {
                    Column(modifier = Modifier.padding(16.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Box(
                                modifier = Modifier
                                    .size(16.dp)
                                    .clip(CircleShape)
                                    .background(if (isRunning) Color(0xFF2ECC71) else Color(0xFFE74C3C))
                            )
                            Spacer(modifier = Modifier.width(10.dp))
                            Text(
                                text = if (isRunning) "Server: Running" else "Server: Stopped",
                                fontWeight = FontWeight.Bold,
                                fontSize = 18.sp,
                                color = MaterialTheme.colorScheme.onSurface
                            )
                        }

                        Spacer(modifier = Modifier.height(6.dp))
                        Text(
                            text = "Device Name: $deviceName",
                            fontSize = 14.sp,
                            fontWeight = FontWeight.Medium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )

                        Spacer(modifier = Modifier.height(4.dp))
                        Text(
                            text = if (isRunning) "http://$currentIp:8484" else "Wi-Fi: $currentIp",
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            fontSize = 14.sp
                        )

                        Spacer(modifier = Modifier.height(16.dp))
                        Row(modifier = Modifier.fillMaxWidth()) {
                            Button(
                                onClick = {
                                    if (isRunning) onStopServer() else onStartServer()
                                    isRunning = !isRunning
                                },
                                modifier = Modifier.weight(1f),
                                colors = ButtonDefaults.buttonColors(
                                    containerColor = if (isRunning) Color(0xFFE74C3C) else MaterialTheme.colorScheme.primary,
                                    contentColor = Color.White
                                )
                            ) {
                                Text(if (isRunning) "Stop Server" else "Start Server")
                            }

                            if (isRunning) {
                                Spacer(modifier = Modifier.width(8.dp))
                                OutlinedButton(
                                    onClick = {
                                        val pin = try {
                                            QSyncBridge.generatePIN()
                                        } catch (e: Exception) {
                                            null
                                        }
                                        if (!pin.isNullOrEmpty()) {
                                            pairPin = pin
                                        } else {
                                            coroutineScope.launch(Dispatchers.IO) {
                                                try {
                                                    val req = Request.Builder().url("http://127.0.0.1:8484/api/pin").build()
                                                    val res = httpClient.newCall(req).execute()
                                                    if (res.isSuccessful) {
                                                        val obj = JSONObject(res.body?.string() ?: "{}")
                                                        val p = obj.optString("pin")
                                                        withContext(Dispatchers.Main) {
                                                            pairPin = p
                                                        }
                                                    }
                                                } catch (_: Exception) {}
                                            }
                                        }
                                    }
                                ) {
                                    Text("Pair PC")
                                }
                            }
                        }

                        // PIN Display Box
                        if (pairPin != null) {
                            Spacer(modifier = Modifier.height(16.dp))
                            Card(
                                colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
                                shape = RoundedCornerShape(8.dp)
                            ) {
                                Column(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .padding(16.dp),
                                    horizontalAlignment = Alignment.CenterHorizontally
                                ) {
                                    Text(
                                        "Pairing PIN Code",
                                        fontSize = 13.sp,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant
                                    )
                                    val p = pairPin!!
                                    val formatted = if (p.length == 6) "${p.substring(0, 3)} ${p.substring(3)}" else p
                                    Text(
                                        formatted,
                                        fontWeight = FontWeight.Bold,
                                        fontSize = 32.sp,
                                        color = MaterialTheme.colorScheme.primary
                                    )
                                    Text(
                                        "Enter this code in GNOME extension or CLI",
                                        fontSize = 12.sp,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant
                                    )
                                    Spacer(modifier = Modifier.height(8.dp))
                                    TextButton(onClick = { pairPin = null }) {
                                        Text("Dismiss")
                                    }
                                }
                            }
                        }
                    }
                }
            }

            // 3. Shared Folders Section
            item {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Text(
                        "Shared Folders",
                        fontWeight = FontWeight.Bold,
                        fontSize = 18.sp,
                        color = MaterialTheme.colorScheme.onBackground
                    )
                    FilledTonalButton(
                        onClick = {
                            if (!isRunning) {
                                folderPickerLauncher.launch(null)
                            }
                        },
                        enabled = !isRunning
                    ) {
                        Icon(Icons.Default.Add, contentDescription = null, modifier = Modifier.size(18.dp))
                        Spacer(modifier = Modifier.width(4.dp))
                        Text("Add Folder")
                    }
                }
            }

            if (folders.isEmpty()) {
                item {
                    Text(
                        "No folders shared. Click '+ Add Folder' to choose one.",
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        fontSize = 14.sp
                    )
                }
            } else {
                items(folders) { folder ->
                    Card(
                        shape = RoundedCornerShape(8.dp),
                        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surface),
                        elevation = CardDefaults.cardElevation(1.dp)
                    ) {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(12.dp),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Icon(
                                Icons.Default.Folder,
                                contentDescription = null,
                                tint = MaterialTheme.colorScheme.primary
                            )
                            Spacer(modifier = Modifier.width(12.dp))
                            Column(modifier = Modifier.weight(1f)) {
                                Text(
                                    folder.name,
                                    fontWeight = FontWeight.SemiBold,
                                    fontSize = 16.sp,
                                    color = MaterialTheme.colorScheme.onSurface
                                )
                                Text(
                                    folder.path,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    fontSize = 13.sp
                                )
                            }
                            IconButton(
                                onClick = {
                                    if (!isRunning) {
                                        val updated = folders.filter { it.id != folder.id }
                                        saveFoldersToConfig(updated)
                                    }
                                },
                                enabled = !isRunning
                            ) {
                                Icon(
                                    Icons.Default.Delete,
                                    contentDescription = "Delete Folder",
                                    tint = if (!isRunning) Color(0xFFE74C3C) else Color.LightGray
                                )
                            }
                        }
                    }
                }
            }
        }
    }

    // Add Folder Confirmation Dialog
    if (showAddDialog) {
        AlertDialog(
            onDismissRequest = { showAddDialog = false },
            title = { Text("Share Folder") },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(
                        value = newFolderName,
                        onValueChange = { newFolderName = it },
                        label = { Text("Folder Name") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth()
                    )
                    Text("Path: $newFolderPath", fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            },
            confirmButton = {
                Button(
                    onClick = {
                        val name = newFolderName.trim().ifEmpty { File(newFolderPath).name }
                        val id = System.currentTimeMillis().toString()
                        val newFolder = SharedFolder(id = id, name = name, path = newFolderPath)
                        val updated = folders + newFolder
                        saveFoldersToConfig(updated)
                        showAddDialog = false
                        newFolderName = ""
                        newFolderPath = ""
                    },
                    enabled = !isRunning
                ) {
                    Text("Add")
                }
            },
            dismissButton = {
                TextButton(onClick = { showAddDialog = false }) {
                    Text("Cancel")
                }
            }
        )
    }

    // Settings Dialog
    if (showSettingsDialog) {
        var tempDeviceName by remember { mutableStateOf(deviceName) }

        AlertDialog(
            onDismissRequest = { showSettingsDialog = false },
            title = {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Default.Settings, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                    Spacer(modifier = Modifier.width(8.dp))
                    Text("Settings", fontWeight = FontWeight.Bold)
                }
            },
            text = {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .verticalScroll(rememberScrollState()),
                    verticalArrangement = Arrangement.spacedBy(16.dp)
                ) {
                    // Theme Selector Section
                    Column {
                        Text(
                            "Theme",
                            fontWeight = FontWeight.SemiBold,
                            fontSize = 15.sp,
                            color = MaterialTheme.colorScheme.onSurface
                        )
                        Spacer(modifier = Modifier.height(6.dp))

                        val modes = listOf(
                            AppThemeMode.SYSTEM to "System Default",
                            AppThemeMode.LIGHT to "Light Theme",
                            AppThemeMode.DARK to "Dark Theme"
                        )

                        modes.forEach { (mode, label) ->
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clickable { onThemeModeChanged(mode) }
                                    .padding(vertical = 4.dp),
                                verticalAlignment = Alignment.CenterVertically
                            ) {
                                RadioButton(
                                    selected = (currentThemeMode == mode),
                                    onClick = { onThemeModeChanged(mode) }
                                )
                                Spacer(modifier = Modifier.width(8.dp))
                                Text(label, fontSize = 14.sp)
                            }
                        }
                    }

                    HorizontalDivider()

                    // Device Name Section
                    Column {
                        Text(
                            "Device Name",
                            fontWeight = FontWeight.SemiBold,
                            fontSize = 15.sp,
                            color = MaterialTheme.colorScheme.onSurface
                        )
                        Spacer(modifier = Modifier.height(4.dp))
                        Text(
                            if (isRunning) "Stop server to change device name" else "Shown to desktop for discovery",
                            fontSize = 12.sp,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                        Spacer(modifier = Modifier.height(8.dp))
                        OutlinedTextField(
                            value = tempDeviceName,
                            onValueChange = { tempDeviceName = it },
                            enabled = !isRunning,
                            singleLine = true,
                            modifier = Modifier.fillMaxWidth()
                        )
                    }

                    HorizontalDivider()

                    // App Version Section
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween
                    ) {
                        Text("Version", fontSize = 13.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Text("1.0.0", fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
                    }
                }
            },
            confirmButton = {
                Button(
                    onClick = {
                        val trimmed = tempDeviceName.trim()
                        if (trimmed.isNotEmpty() && trimmed != deviceName && !isRunning) {
                            saveDeviceNameToConfig(trimmed)
                        }
                        showSettingsDialog = false
                    }
                ) {
                    Text("Close")
                }
            }
        )
    }
}
