import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import * as QuickSettings from 'resource:///org/gnome/shell/ui/quickSettings.js';
import * as ModalDialog from 'resource:///org/gnome/shell/ui/modalDialog.js';
import GObject from 'gi://GObject';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import Clutter from 'gi://Clutter';
import St from 'gi://St';

const QSyncIface = `
<node>
  <interface name="org.qsync.Daemon">
    <method name="GetStatus">
      <arg type="b" direction="out" name="phoneFound"/>
      <arg type="s" direction="out" name="phoneName"/>
      <arg type="s" direction="out" name="phoneIP"/>
      <arg type="i" direction="out" name="phonePort"/>
      <arg type="b" direction="out" name="isMounted"/>
    </method>
    <method name="Pair">
      <arg type="s" direction="in" name="pin"/>
      <arg type="s" direction="in" name="ip"/>
      <arg type="i" direction="in" name="port"/>
      <arg type="b" direction="out" name="success"/>
      <arg type="s" direction="out" name="serverName"/>
      <arg type="s" direction="out" name="errorMsg"/>
    </method>
    <method name="Mount">
      <arg type="b" direction="out" name="success"/>
      <arg type="s" direction="out" name="message"/>
    </method>
    <method name="Unmount">
      <arg type="b" direction="out" name="success"/>
      <arg type="s" direction="out" name="message"/>
    </method>
    <method name="OpenInNautilus">
      <arg type="b" direction="out" name="success"/>
    </method>
    <method name="GetDevicesJSON">
      <arg type="s" direction="out" name="jsonString"/>
    </method>
    <method name="MountDevice">
      <arg type="s" direction="in" name="name"/>
      <arg type="s" direction="in" name="ip"/>
      <arg type="i" direction="in" name="port"/>
      <arg type="b" direction="out" name="success"/>
      <arg type="s" direction="out" name="message"/>
    </method>
    <method name="UnmountDevice">
      <arg type="s" direction="in" name="name"/>
      <arg type="s" direction="in" name="ip"/>
      <arg type="i" direction="in" name="port"/>
      <arg type="b" direction="out" name="success"/>
      <arg type="s" direction="out" name="message"/>
    </method>
    <method name="OpenDeviceInFileManager">
      <arg type="s" direction="in" name="name"/>
      <arg type="s" direction="in" name="ip"/>
      <arg type="i" direction="in" name="port"/>
      <arg type="b" direction="out" name="success"/>
    </method>
    <method name="GetSyncFolders">
      <arg type="s" direction="in" name="name"/>
      <arg type="s" direction="in" name="ip"/>
      <arg type="i" direction="in" name="port"/>
      <arg type="s" direction="out" name="jsonString"/>
    </method>
    <method name="SetSyncFolderMapping">
      <arg type="s" direction="in" name="name"/>
      <arg type="s" direction="in" name="remoteFolder"/>
      <arg type="s" direction="in" name="localPath"/>
      <arg type="b" direction="out" name="success"/>
    </method>
    <method name="SetSyncFolderMappings">
      <arg type="s" direction="in" name="name"/>
      <arg type="s" direction="in" name="jsonMappings"/>
      <arg type="b" direction="out" name="success"/>
    </method>
    <method name="SyncDevice">
      <arg type="s" direction="in" name="name"/>
      <arg type="s" direction="in" name="ip"/>
      <arg type="i" direction="in" name="port"/>
      <arg type="b" direction="out" name="success"/>
      <arg type="s" direction="out" name="summaryMsg"/>
      <arg type="s" direction="out" name="errorMsg"/>
    </method>
    <signal name="StatusChanged">
      <arg type="b" name="phoneFound"/>
      <arg type="b" name="isMounted"/>
    </signal>
    <signal name="SyncStarted">
      <arg type="s" name="name"/>
    </signal>
    <signal name="SyncCompleted">
      <arg type="s" name="name"/>
      <arg type="b" name="success"/>
      <arg type="s" name="message"/>
    </signal>
  </interface>
</node>`;

const QSyncProxy = Gio.DBusProxy.makeProxyWrapper(QSyncIface);

// Modal Pairing Dialog
const QSyncPairDialog = GObject.registerClass(
class QSyncPairDialog extends ModalDialog.ModalDialog {
    _init(extension, targetDev = null) {
        super._init({ styleClass: 'qsync-pair-dialog' });
        this._ext = extension;
        this._targetDev = targetDev;

        const content = new St.BoxLayout({
            vertical: true,
            style: 'padding: 20px; spacing: 14px; min-width: 320px;',
        });

        const devTitle = targetDev ? `Pair with ${targetDev.name || 'Phone'}` : 'Pair with Phone';
        const title = new St.Label({
            text: devTitle,
            style: 'font-weight: bold; font-size: 1.3em; text-align: center;',
        });
        content.add_child(title);

        const desc = new St.Label({
            text: 'Enter the 6-digit PIN displayed on your phone:',
            style: 'text-align: center; color: #9a9996;',
        });
        content.add_child(desc);

        this._entry = new St.Entry({
            hint_text: 'PIN code (e.g. 123456)',
            can_focus: true,
            style: 'font-size: 1.4em; text-align: center; padding: 10px;',
        });
        content.add_child(this._entry);

        this._errorLabel = new St.Label({
            text: '',
            style: 'color: #e01b24; font-size: 0.9em; text-align: center;',
        });
        this._errorLabel.visible = false;
        content.add_child(this._errorLabel);

        this.contentLayout.add_child(content);

        this.addButton({
            label: 'Cancel',
            action: () => this.close(),
            key: Clutter.KEY_Escape,
        });

        this._pairButton = this.addButton({
            label: 'Pair',
            action: () => this._onPair(),
            isDefault: true,
            key: Clutter.KEY_Return,
        });
    }

    _onPair() {
        const pin = this._entry.get_text().trim();
        if (pin.length !== 6) {
            this._errorLabel.text = 'PIN must be exactly 6 digits';
            this._errorLabel.visible = true;
            return;
        }

        this._errorLabel.visible = false;
        this._pairButton.reactive = false;
        this._pairButton.label = 'Pairing...';

        const targetIP = this._targetDev ? this._targetDev.ip : (this._ext._phoneIP || '');
        const targetPort = this._targetDev ? (this._targetDev.port || 8484) : 8484;

        this._ext.pairWithPhone(pin, targetIP, targetPort, (success, serverName, errorMsg) => {
            if (success) {
                this.close();
                Main.notify('qSync', `Successfully paired with ${serverName}!`);
            } else {
                this._pairButton.reactive = true;
                this._pairButton.label = 'Pair';
                this._errorLabel.text = errorMsg || 'Pairing failed. Check PIN.';
                this._errorLabel.visible = true;
            }
        });
    }
});

// Modal Sync Settings Dialog
const QSyncSettingsDialog = GObject.registerClass(
class QSyncSettingsDialog extends ModalDialog.ModalDialog {
    _init(extension, targetDev) {
        super._init({ styleClass: 'qsync-settings-dialog' });
        this._ext = extension;
        this._targetDev = targetDev;
        this._entries = {};

        const content = new St.BoxLayout({
            vertical: true,
            style: 'padding: 20px; spacing: 14px; min-width: 560px; max-width: 720px;',
        });

        const devName = targetDev ? (targetDev.name || 'Phone') : 'Phone';
        const title = new St.Label({
            text: `Sync Settings - ${devName}`,
            style: 'font-weight: bold; font-size: 1.3em; text-align: center;',
        });
        content.add_child(title);

        const desc = new St.Label({
            text: 'Map each phone sync folder to a desktop source folder (Desktop ➔ Phone One-Way Sync):',
            style: 'color: #9a9996; font-size: 0.95em;',
        });
        content.add_child(desc);

        this._foldersContainer = new St.BoxLayout({
            vertical: true,
            style: 'spacing: 12px; margin-top: 10px;',
        });
        content.add_child(this._foldersContainer);

        const loadingLabel = new St.Label({
            text: 'Loading syncable folders from phone...',
            style: 'color: #9a9996; font-style: italic;',
        });
        this._foldersContainer.add_child(loadingLabel);

        this.contentLayout.add_child(content);

        this.addButton({
            label: 'Cancel',
            action: () => this.close(),
            key: Clutter.KEY_Escape,
        });

        this._saveButton = this.addButton({
            label: 'Save',
            action: () => this._onSave(),
            isDefault: true,
            key: Clutter.KEY_Return,
        });

        this._loadFolders();
    }

    _loadFolders() {
        if (!this._ext._proxy) return;

        const devName = this._targetDev ? this._targetDev.name : '';
        const devIP = this._targetDev ? this._targetDev.ip : '';
        const devPort = this._targetDev ? (this._targetDev.port || 8484) : 8484;

        this._ext._proxy.GetSyncFoldersRemote(devName, devIP, devPort, (result, err) => {
            this._foldersContainer.destroy_all_children();
            if (err || !result || !result[0]) {
                const errLabel = new St.Label({
                    text: 'Failed to retrieve sync folders from phone. Ensure phone is online and paired.',
                    style: 'color: #e01b24;',
                });
                this._foldersContainer.add_child(errLabel);
                return;
            }

            let folders = [];
            try {
                folders = JSON.parse(result[0]);
            } catch (e) {
                folders = [];
            }

            if (folders.length === 0) {
                const emptyLabel = new St.Label({
                    text: 'No syncable folders found on phone. In the phone app, enable "Syncable folder" when sharing a folder.',
                    style: 'color: #9a9996; font-style: italic;',
                });
                this._foldersContainer.add_child(emptyLabel);
                return;
            }

            folders.forEach(f => {
                const folderBox = new St.BoxLayout({
                    vertical: true,
                    style: 'spacing: 6px; padding: 10px; background-color: rgba(255,255,255,0.06); border-radius: 8px;',
                });

                const headerBox = new St.BoxLayout({ vertical: false, style: 'spacing: 8px;' });
                const icon = new St.Icon({ icon_name: 'folder-symbolic', icon_size: 16 });
                const nameLabel = new St.Label({
                    text: `Phone Target: ${f.name}`,
                    style: 'font-weight: bold; font-size: 1.05em;',
                });
                headerBox.add_child(icon);
                headerBox.add_child(nameLabel);
                folderBox.add_child(headerBox);

                const rowBox = new St.BoxLayout({
                    vertical: false,
                    style: 'spacing: 8px; margin-top: 4px;',
                });

                const entry = new St.Entry({
                    text: f.localPath || '',
                    hint_text: 'Not synced (click Browse or type path)',
                    can_focus: true,
                    style: 'padding: 6px 10px; font-size: 0.95em; min-width: 310px;',
                });
                this._entries[f.name] = entry;
                rowBox.add_child(entry);

                const browseBtn = new St.Button({
                    label: 'Browse...',
                    style_class: 'button',
                    style: 'padding: 6px 12px;',
                    can_focus: true,
                });
                browseBtn.connect('clicked', () => {
                    this._onBrowse(f.name, entry, browseBtn);
                });
                rowBox.add_child(browseBtn);

                const dontSyncBtn = new St.Button({
                    label: "Don't sync",
                    style_class: 'button',
                    style: 'padding: 6px 12px;',
                    can_focus: true,
                });
                dontSyncBtn.connect('clicked', () => {
                    entry.set_text('');
                });
                rowBox.add_child(dontSyncBtn);

                folderBox.add_child(rowBox);
                this._foldersContainer.add_child(folderBox);
            });
        });
    }

    _onBrowse(folderName, entry, browseBtn) {
        if (browseBtn) browseBtn.reactive = false;

        const hadModal = this._hasModal;
        if (hadModal) {
            this.popModal();
        }
        // Temporarily hide the modal dialog so the external Zenity file picker
        // is directly visible in the foreground (GNOME Shell modalDialogGroup sits above all client windows)
        this.hide();

        const restoreModal = () => {
            if (browseBtn) browseBtn.reactive = true;
            this.show();
            if (hadModal && !this._hasModal) {
                this.pushModal();
            }
        };

        try {
            const title = `Select desktop source folder for ${folderName}`;
            const launcher = new Gio.SubprocessLauncher({
                flags: Gio.SubprocessFlags.STDOUT_PIPE | Gio.SubprocessFlags.STDERR_PIPE,
            });
            launcher.set_environ(GLib.get_environ());
            launcher.setenv('GDK_BACKEND', 'x11', true);

            const argv = [
                'zenity',
                '--file-selection',
                '--directory',
                '--modal',
                `--title=${title}`,
            ];

            const currentPath = entry.get_text().trim();
            if (currentPath && GLib.file_test(currentPath, GLib.FileTest.IS_DIR)) {
                argv.push(`--filename=${currentPath}/`);
            }

            const proc = launcher.spawnv(argv);

            proc.communicate_utf8_async(null, null, (p, res) => {
                try {
                    const [ok, stdout] = p.communicate_utf8_finish(res);
                    if (ok && stdout) {
                        const selectedPath = stdout.trim();
                        if (selectedPath) {
                            entry.set_text(selectedPath);
                        }
                    }
                } catch (e) {
                    // User cancelled or closed dialog
                } finally {
                    restoreModal();
                }
            });
        } catch (e) {
            restoreModal();
        }
    }

    _onSave() {
        if (!this._ext._proxy) return;
        const devName = this._targetDev ? this._targetDev.name : '';
        const mappings = {};
        const forbiddenPaths = new Set(['/', '/home', '/etc', '/usr', '/var', '/bin', '/boot', '/root', GLib.get_home_dir()]);

        for (const [folderName, entry] of Object.entries(this._entries)) {
            const path = entry.get_text().trim();
            if (path) {
                const clean = path.replace(/\/+$/, '') || '/';
                if (forbiddenPaths.has(clean)) {
                    Main.notify('qSync Error', `Cannot map '${clean}'. System roots and home directory cannot be synced.`);
                    return;
                }
            }
            mappings[folderName] = path;
        }

        this._ext._proxy.SetSyncFolderMappingsRemote(devName, JSON.stringify(mappings), () => {
            if (this._ext) {
                this._ext._refreshStatus();
            }
        });

        this.close();
        Main.notify('qSync', 'Sync folder settings saved.');
    }
});

// Quick Settings Toggle Widget
const QSyncQuickToggle = GObject.registerClass(
class QSyncQuickToggle extends QuickSettings.QuickMenuToggle {
    _init(extension) {
        super._init({
            title: 'qSync',
            iconName: 'phone-symbolic',
            toggleMode: true,
        });

        this._isQSyncToggle = true;
        this._ext = extension;
        this.menu.setHeader('phone-symbolic', 'qSync for Android', 'Local Wi-Fi file sync');

        this._menuSection = new PopupMenu.PopupMenuSection();
        this.menu.addMenuItem(this._menuSection);

        this._statusLabel = new PopupMenu.PopupMenuItem('Status: Off', {reactive: false});
        this._menuSection.addMenuItem(this._statusLabel);

        this._pairAction = new PopupMenu.PopupMenuItem('🤝 Pair Phone...');
        this._pairAction.connect('activate', () => this._ext.showPairDialog());
        this._menuSection.addMenuItem(this._pairAction);

        this._mountAction = new PopupMenu.PopupMenuItem('🔗 Mount Phone');
        this._mountAction.connect('activate', () => this._ext.mountPhone());
        this._menuSection.addMenuItem(this._mountAction);

        this._openAction = new PopupMenu.PopupMenuItem('📂 Open in File Manager');
        this._openAction.connect('activate', () => this._ext.openFileManager());
        this._menuSection.addMenuItem(this._openAction);

        this._unmountAction = new PopupMenu.PopupMenuItem('⏏ Unmount Phone');
        this._unmountAction.connect('activate', () => this._ext.unmountPhone());
        this._menuSection.addMenuItem(this._unmountAction);

        this.connect('clicked', () => {
            this._ext.toggleActive(this.checked);
        });
    }

    destroy() {
        if (this.menu) {
            this.menu.destroy();
            this.menu = null;
        }
        super.destroy();
    }

    updateState(active, phoneFound, phoneName, phoneIP, phonePort, isMounted, isPaired = false) {
        this.checked = active;

        if (!active) {
            this.subtitle = 'Off';
            this._statusLabel.label.text = 'Daemon stopped';
            this._pairAction.visible = false;
            this._mountAction.visible = false;
            this._openAction.visible = false;
            this._unmountAction.visible = false;
            return;
        }

        // Show Pair action whenever phone is found and not mounted
        this._pairAction.visible = phoneFound && !isMounted;
        if (this._pairAction.label) {
            this._pairAction.label.text = isPaired ? '🔄 Re-pair Phone...' : '🤝 Pair Phone...';
        }

        if (isMounted) {
            this.subtitle = 'Mounted';
            this._statusLabel.label.text = `${phoneName} (${phoneIP}:${phonePort}) - Mounted`;
            this._mountAction.visible = false;
            this._openAction.visible = true;
            this._unmountAction.visible = true;
        } else if (phoneFound) {
            if (isPaired) {
                this.subtitle = phoneName || 'Ready';
                this._statusLabel.label.text = `${phoneName} (${phoneIP}:${phonePort}) - Ready`;
            } else {
                this.subtitle = 'Pairing Required';
                this._statusLabel.label.text = `${phoneName} (${phoneIP}:${phonePort}) - Pairing Required`;
            }
            this._mountAction.visible = isPaired;
            this._openAction.visible = false;
            this._unmountAction.visible = false;
        } else {
            this.subtitle = 'Searching...';
            this._statusLabel.label.text = 'Searching for phone on Wi-Fi...';
            this._mountAction.visible = false;
            this._openAction.visible = false;
            this._unmountAction.visible = false;
        }
    }
});

// Quick Settings System Indicator
const QSyncSystemIndicator = GObject.registerClass(
class QSyncSystemIndicator extends QuickSettings.SystemIndicator {
    _init(extension) {
        super._init();
        this._isQSyncIndicator = true;
        this.quickSettingsToggle = new QSyncQuickToggle(extension);
        this.quickSettingsItems.push(this.quickSettingsToggle);
    }

    destroy() {
        this.quickSettingsItems.forEach(item => {
            if (item) {
                item.destroy();
            }
        });
        this.quickSettingsItems = [];
        this.quickSettingsToggle = null;
        super.destroy();
    }
});

export default class QSyncExtension extends Extension {
    enable() {
        this._isActive = false;
        this._phoneIP = '';
        this._phonePort = 8484;
        this._daemonProc = null;
        this._pairDialog = null;
        this._settingsDialog = null;
        this._syncingDevices = new Set();
        this._lastDevices = [];

        // Clean up any preexisting/orphaned toggles in Quick Settings grid (e.g. from previous reloads)
        this._cleanupQuickSettings();

        // 1. Top Bar Panel Indicator
        this._indicator = new PanelMenu.Button(0.0, this.metadata.name, false);
        this._indicator.visible = false;

        this._icon = new St.Icon({
            icon_name: 'phone-symbolic',
            style_class: 'system-status-icon qsync-indicator-offline',
        });
        this._indicator.add_child(this._icon);

        this._buildPanelMenu();
        Main.panel.addToStatusArea(this.uuid, this._indicator);

        // 2. Quick Settings Toggle
        this._qsIndicator = new QSyncSystemIndicator(this);
        Main.panel.statusArea.quickSettings.addExternalIndicator(this._qsIndicator);

        this._initDBus();
    }

    disable() {
        this._stopPolling();

        if (this._pairDialog) {
            this._pairDialog.close();
            this._pairDialog = null;
        }

        if (this._settingsDialog) {
            this._settingsDialog.close();
            this._settingsDialog = null;
        }

        if (this._signalId && this._proxy) {
            this._proxy.disconnectSignal(this._signalId);
            this._signalId = null;
        }

        if (this._syncStartedSignalId && this._proxy) {
            this._proxy.disconnectSignal(this._syncStartedSignalId);
            this._syncStartedSignalId = null;
        }

        if (this._syncSignalId && this._proxy) {
            this._proxy.disconnectSignal(this._syncSignalId);
            this._syncSignalId = null;
        }

        if (this._indicator) {
            this._indicator.destroy();
            this._indicator = null;
        }

        if (this._qsIndicator) {
            this._qsIndicator.destroy();
            this._qsIndicator = null;
        }

        this._cleanupQuickSettings();

        this._syncingDevices = null;
        this._lastDevices = null;
        this._proxy = null;
    }

    _cleanupQuickSettings() {
        try {
            const qsMenu = Main.panel.statusArea.quickSettings?.menu;
            if (qsMenu && qsMenu._grid) {
                const itemsToRemove = [];
                for (const child of qsMenu._grid.get_children()) {
                    if (this._qsIndicator && child === this._qsIndicator.quickSettingsToggle)
                        continue;
                    if (child.title === 'qSync' || child._isQSyncToggle || child._ext !== undefined || child.constructor?.name === 'QSyncQuickToggle') {
                        itemsToRemove.push(child);
                    }
                }
                for (const item of itemsToRemove) {
                    if (item.menu) {
                        try { item.menu.destroy(); } catch (e) {}
                    }
                    try { item.destroy(); } catch (e) {}
                }
            }
        } catch (e) {
            console.error('qSync: error during quick settings cleanup', e);
        }

        try {
            const indicatorsBox = Main.panel.statusArea.quickSettings?._indicators;
            if (indicatorsBox) {
                const indicatorsToRemove = [];
                for (const child of indicatorsBox.get_children()) {
                    if (child === this._qsIndicator)
                        continue;
                    if (child._isQSyncIndicator || child.constructor?.name === 'QSyncSystemIndicator') {
                        indicatorsToRemove.push(child);
                    }
                }
                for (const ind of indicatorsToRemove) {
                    try { ind.destroy(); } catch (e) {}
                }
            }
        } catch (e) {}
    }

    _buildPanelMenu() {
        this._menuHeader = new PopupMenu.PopupMenuItem('qSync', {reactive: false});
        this._indicator.menu.addMenuItem(this._menuHeader);

        this._devicesSection = new PopupMenu.PopupMenuSection();
        this._indicator.menu.addMenuItem(this._devicesSection);

        this._renderedDevicesKey = '';
    }

    showPairDialog(targetDev = null) {
        if (this._pairDialog) {
            this._pairDialog.close();
            this._pairDialog = null;
        }
        this._pairDialog = new QSyncPairDialog(this, targetDev);
        this._pairDialog.connect('closed', () => {
            this._pairDialog = null;
        });
        this._pairDialog.open();
    }

    pairWithPhone(pin, ip = '', port = 8484, callback = null) {
        if (!this._proxy) {
            if (callback) callback(false, '', 'Daemon not running. Turn on qSync first.');
            return;
        }
        const targetIP = ip || this._phoneIP || '';
        const targetPort = port || 8484;
        this._proxy.PairRemote(pin, targetIP, targetPort, (result, err) => {
            if (err || !result) {
                if (callback) callback(false, '', err ? err.message : 'Unknown error');
                return;
            }
            const [success, serverName, errorMsg] = result;
            if (success) {
                this._refreshStatus();
            }
            if (callback) callback(success, serverName, errorMsg);
        });
    }

    toggleActive(active) {
        this._isActive = active;

        if (this._indicator) {
            this._indicator.visible = active;
        }

        if (active) {
            this._startDaemon();
            this._startPolling();
        } else {
            this._stopDaemon();
            this._stopPolling();
            if (this._syncingDevices) {
                this._syncingDevices.clear();
            }
            this._renderedDevicesKey = '';
            this._updateUIState([]);
        }
    }

    _startDaemon() {
        try {
            GLib.spawn_command_line_async('systemctl --user start qsync.service');
        } catch (e) {
            try {
                const [success, argv] = GLib.shell_parse_argv('qsync daemon');
                if (success) {
                    const launcher = new Gio.SubprocessLauncher({
                        flags: Gio.SubprocessFlags.NONE,
                    });
                    this._daemonProc = launcher.spawnv(argv);
                }
            } catch (err) {}
        }
    }

    _stopDaemon() {
        // Also force-unmount any dav mounts for qsync in user session safely
        try {
            if (this._lastDevices && this._lastDevices.length > 0) {
                const ipRegex = /^[a-zA-Z0-9.:%-]+$/;
                const launcher = new Gio.SubprocessLauncher({
                    flags: Gio.SubprocessFlags.NONE,
                });
                this._lastDevices.forEach(d => {
                    if (d.ip && ipRegex.test(d.ip)) {
                        const port = Number.isInteger(d.port) && d.port > 0 && d.port <= 65535 ? d.port : 8484;
                        try {
                            launcher.spawnv(['gio', 'mount', '-u', '-f', `dav://qsync@${d.ip}:${port}/`]);
                        } catch (err) {}
                        try {
                            launcher.spawnv(['gio', 'mount', '-u', '-f', `dav://${d.ip}:${port}/`]);
                        } catch (err) {}
                    }
                });
            }
        } catch (e) {}

        try {
            GLib.spawn_command_line_async('systemctl --user stop qsync.service');
        } catch (e) {}

        if (this._daemonProc) {
            try {
                this._daemonProc.force_exit();
            } catch (e) {}
            this._daemonProc = null;
        }
    }

    _checkInitialDaemon() {
        if (!this._proxy) return;

        this._proxy.GetDevicesJSONRemote((result, error) => {
            if (!error && result !== undefined) {
                this._isActive = true;
                this._indicator.visible = true;
                this._startPolling();
                this._refreshStatus();
            }
        });
    }

    _startPolling() {
        if (this._timerSource) return;
        this._refreshStatus();
        this._timerSource = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 3, () => {
            this._refreshStatus();
            return GLib.SOURCE_CONTINUE;
        });
    }

    _stopPolling() {
        if (this._timerSource) {
            GLib.source_remove(this._timerSource);
            this._timerSource = null;
        }
    }

    _initDBus() {
        try {
            this._proxy = new QSyncProxy(
                Gio.DBus.session,
                'org.qsync.Daemon',
                '/org/qsync/Daemon',
                (proxy, error) => {
                    if (error) {
                        return;
                    }
                    this._isActive = true;
                    if (this._indicator) {
                        this._indicator.visible = true;
                    }
                    this._signalId = proxy.connectSignal('StatusChanged', () => {
                        this._refreshStatus();
                    });
                    this._syncStartedSignalId = proxy.connectSignal('SyncStarted', (_proxy, _sender, [device]) => {
                        if (this._syncingDevices) {
                            this._syncingDevices.add(device);
                        }
                        this._renderedDevicesKey = '';
                        this._refreshStatus();
                    });
                    this._syncSignalId = proxy.connectSignal('SyncCompleted', (_proxy, _sender, [device, success, message]) => {
                        if (this._syncingDevices) {
                            this._syncingDevices.delete(device);
                        }
                        this._renderedDevicesKey = '';
                        this._refreshStatus();
                    });
                    this._startPolling();
                }
            );
        } catch (e) {}
    }

    _refreshStatus() {
        if (!this._proxy) {
            this._initDBus();
            return;
        }

        this._proxy.GetDevicesJSONRemote((result, error) => {
            if (error) {
                // If daemon is starting up or restarting, attempt to reconnect
                this._updateUIState([]);
                return;
            }
            if (!result || !result[0]) {
                this._updateUIState([]);
                return;
            }

            let devices = [];
            try {
                devices = JSON.parse(result[0]);
            } catch (e) {
                devices = [];
            }
            this._updateUIState(devices);
        });
    }

    _updateUIState(devices = []) {
        this._lastDevices = devices;
        if (this._syncingDevices) {
            devices.forEach(d => {
                if (d.isSyncing) {
                    this._syncingDevices.add(d.name);
                } else if (!d.isOnline) {
                    // Device went offline -> clear sync state
                    this._syncingDevices.delete(d.name);
                }
            });
        }
        const anyMounted = devices.some(d => d.isMounted);
        const anyOnline = devices.some(d => d.isOnline);
        const isAnySyncing = devices.some(d => d.isSyncing) || (this._syncingDevices && this._syncingDevices.size > 0);

        // Find primary device for quick toggle
        const primary = devices.find(d => d.isMounted) || devices.find(d => d.isOnline) || (devices.length > 0 ? devices[0] : null);

        if (primary && primary.ip) {
            this._phoneIP = primary.ip;
            this._phonePort = primary.port || 8484;
        }

        // 1. Update Quick Settings Toggle
        if (this._qsIndicator && this._qsIndicator.quickSettingsToggle) {
            this._qsIndicator.quickSettingsToggle.updateState(
                this._isActive,
                anyOnline,
                primary ? primary.name : '',
                primary ? primary.ip : '',
                primary ? primary.port : 8484,
                anyMounted,
                primary ? !!primary.isPaired : false
            );
        }

        // 2. Update Top Bar Panel Indicator Icon
        if (this._indicator) {
            this._indicator.visible = this._isActive;
        }
        if (!this._icon || !this._menuHeader) return;

        this._icon.style_class = 'system-status-icon';
        if (isAnySyncing) {
            this._icon.icon_name = 'emblem-synchronizing-symbolic';
            this._icon.add_style_class_name(anyMounted ? 'qsync-indicator-mounted' : 'qsync-indicator-online');
        } else if (anyMounted) {
            this._icon.icon_name = 'drive-harddisk-symbolic';
            this._icon.add_style_class_name('qsync-indicator-mounted');
        } else if (anyOnline) {
            this._icon.icon_name = 'phone-symbolic';
            this._icon.add_style_class_name('qsync-indicator-online');
        } else {
            this._icon.icon_name = 'phone-symbolic';
            this._icon.add_style_class_name('qsync-indicator-offline');
        }

        // Update Top Bar Header (clean & static to avoid flicker)
        if (devices.length > 1) {
            const onlineCount = devices.filter(d => d.isOnline).length;
            this._menuHeader.label.text = `qSync • ${onlineCount}/${devices.length} Online`;
        } else if (anyMounted) {
            this._menuHeader.label.text = 'qSync • Mounted';
        } else if (anyOnline) {
            const anyPaired = devices.some(d => d.isOnline && d.isPaired);
            this._menuHeader.label.text = anyPaired ? 'qSync • Ready' : 'qSync • Pairing Required';
        } else {
            this._menuHeader.label.text = 'qSync • Searching Wi-Fi...';
        }

        // 3. Render Devices in Menu with cache key check to eliminate flicker
        if (this._devicesSection) {
            const newKey = JSON.stringify(devices.map(d => `${d.name}_${d.ip}_${d.isOnline}_${d.isMounted}_${d.isPaired}_${d.hasSyncFolders}_${d.isSyncing || (this._syncingDevices && this._syncingDevices.has(d.name))}`));
            if (newKey !== this._renderedDevicesKey) {
                this._renderedDevicesKey = newKey;
                this._devicesSection.removeAll();

                if (devices.length === 0) {
                    const emptyItem = new PopupMenu.PopupMenuItem('No devices detected on Wi-Fi', {reactive: false});
                    this._devicesSection.addMenuItem(emptyItem);
                } else {
                    devices.forEach(dev => {
                        this._addDeviceMenuItems(this._devicesSection, dev);
                    });
                }
            }
        }
    }

    _addDeviceMenuItems(section, dev) {
        let statusText = 'Offline';
        if (dev.isMounted) {
            statusText = 'Mounted';
        } else if (dev.isOnline) {
            statusText = dev.isPaired ? 'Ready' : 'Unpaired';
        }

        const isThisSyncing = !!dev.isSyncing || (this._syncingDevices && this._syncingDevices.has(dev.name));
        const headerTitle = `📱 ${dev.name} (${dev.ip}) • ${statusText}`;
        const headerItem = new PopupMenu.PopupMenuItem(headerTitle, {reactive: false});
        section.addMenuItem(headerItem);

        if (dev.isMounted) {
            const openItem = new PopupMenu.PopupMenuItem('    📂 Open in Files');
            openItem.connect('activate', () => this.openDeviceInFileManager(dev.name, dev.ip, dev.port));
            section.addMenuItem(openItem);

            if (dev.hasSyncFolders) {
                if (isThisSyncing) {
                    const syncingItem = new PopupMenu.PopupMenuItem('    ⏳ Sync in progress...', {reactive: false});
                    section.addMenuItem(syncingItem);
                } else {
                    const syncNowItem = new PopupMenu.PopupMenuItem('    🔄 Sync Now');
                    syncNowItem.connect('activate', () => this.syncDevice(dev.name, dev.ip, dev.port));
                    section.addMenuItem(syncNowItem);
                }

                const syncSettingsItem = new PopupMenu.PopupMenuItem('    ⚙️ Sync Settings...');
                syncSettingsItem.connect('activate', () => this.showSyncSettingsDialog(dev));
                section.addMenuItem(syncSettingsItem);
            }

            const unmountItem = new PopupMenu.PopupMenuItem('    ⏏ Unmount');
            unmountItem.connect('activate', () => this.unmountDevice(dev.name, dev.ip, dev.port));
            section.addMenuItem(unmountItem);
        } else if (dev.isOnline) {
            if (!dev.isPaired) {
                // Show Pair button for found and unpaired phones
                const pairItem = new PopupMenu.PopupMenuItem('    🤝 Pair Phone...');
                pairItem.connect('activate', () => this.showPairDialog(dev));
                section.addMenuItem(pairItem);
            } else {
                const mountItem = new PopupMenu.PopupMenuItem('    🔗 Mount');
                mountItem.connect('activate', () => this.mountDevice(dev.name, dev.ip, dev.port));
                section.addMenuItem(mountItem);

                if (dev.hasSyncFolders) {
                    if (isThisSyncing) {
                        const syncingItem = new PopupMenu.PopupMenuItem('    ⏳ Sync in progress...', {reactive: false});
                        section.addMenuItem(syncingItem);
                    } else {
                        const syncNowItem = new PopupMenu.PopupMenuItem('    🔄 Sync Now');
                        syncNowItem.connect('activate', () => this.syncDevice(dev.name, dev.ip, dev.port));
                        section.addMenuItem(syncNowItem);
                    }

                    const syncSettingsItem = new PopupMenu.PopupMenuItem('    ⚙️ Sync Settings...');
                    syncSettingsItem.connect('activate', () => this.showSyncSettingsDialog(dev));
                    section.addMenuItem(syncSettingsItem);
                }

                const rePairItem = new PopupMenu.PopupMenuItem('    🔄 Re-pair Phone...');
                rePairItem.connect('activate', () => this.showPairDialog(dev));
                section.addMenuItem(rePairItem);
            }
        }
    }

    showSyncSettingsDialog(targetDev = null) {
        if (this._settingsDialog) {
            this._settingsDialog.close();
            this._settingsDialog = null;
        }
        this._settingsDialog = new QSyncSettingsDialog(this, targetDev);
        this._settingsDialog.connect('closed', () => {
            this._settingsDialog = null;
        });
        this._settingsDialog.open();
    }

    syncDevice(name, ip, port) {
        if (!this._proxy) return;
        if (this._syncingDevices) {
            this._syncingDevices.add(name);
        }
        this._renderedDevicesKey = '';
        if (this._lastDevices) {
            this._updateUIState(this._lastDevices);
        }
        this._proxy.SyncDeviceRemote(name, ip || '', port || 8484, (result, err) => {
            if (err) {
                if (this._syncingDevices) {
                    this._syncingDevices.delete(name);
                }
                this._renderedDevicesKey = '';
                this._refreshStatus();
            }
        });
    }

    mountPhone() {
        if (this._proxy) {
            this._proxy.MountRemote((result, err) => {
                this._refreshStatus();
            });
        }
    }

    unmountPhone() {
        if (this._proxy) {
            this._proxy.UnmountRemote((result, err) => {
                this._refreshStatus();
            });
        }
    }

    mountDevice(name, ip, port) {
        if (this._proxy) {
            this._proxy.MountDeviceRemote(name, ip, port, (result, err) => {
                this._refreshStatus();
            });
        }
    }

    unmountDevice(name, ip, port) {
        if (this._proxy) {
            this._proxy.UnmountDeviceRemote(name, ip, port, (result, err) => {
                this._refreshStatus();
            });
        }
    }

    openDeviceInFileManager(name, ip, port) {
        const targetIP = ip || this._phoneIP || '192.168.3.40';
        const targetPort = port || this._phonePort || 8484;
        const uri = `dav://qsync@${targetIP}:${targetPort}/`;
        try {
            Gio.AppInfo.launch_default_for_uri(uri, null);
        } catch (e) {
            if (this._proxy) {
                this._proxy.OpenDeviceInFileManagerRemote(name || '', targetIP, targetPort, (result, err) => {});
            }
        }
    }

    openFileManager() {
        this.openDeviceInFileManager('', this._phoneIP, this._phonePort);
    }
}
