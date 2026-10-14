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
    <signal name="StatusChanged">
      <arg type="b" name="phoneFound"/>
      <arg type="b" name="isMounted"/>
    </signal>
  </interface>
</node>`;

const QSyncProxy = Gio.DBusProxy.makeProxyWrapper(QSyncIface);

// Modal Pairing Dialog
const QSyncPairDialog = GObject.registerClass(
class QSyncPairDialog extends ModalDialog.ModalDialog {
    _init(extension) {
        super._init({ styleClass: 'qsync-pair-dialog' });
        this._ext = extension;

        const content = new St.BoxLayout({
            vertical: true,
            style: 'padding: 20px; spacing: 14px; min-width: 320px;',
        });

        const title = new St.Label({
            text: 'Pair with Phone',
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

        this._ext.pairWithPhone(pin, (success, serverName, errorMsg) => {
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

// Quick Settings Toggle Widget
const QSyncQuickToggle = GObject.registerClass(
class QSyncQuickToggle extends QuickSettings.QuickMenuToggle {
    _init(extension) {
        super._init({
            title: 'qSync',
            iconName: 'phone-symbolic',
            toggleMode: true,
        });

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

    updateState(active, phoneFound, phoneName, phoneIP, phonePort, isMounted) {
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

        this._pairAction.visible = true;

        if (isMounted) {
            this.subtitle = 'Mounted';
            this._statusLabel.label.text = `${phoneName} (${phoneIP}:${phonePort}) - Mounted`;
            this._mountAction.visible = false;
            this._openAction.visible = true;
            this._unmountAction.visible = true;
        } else if (phoneFound) {
            this.subtitle = phoneName || 'Ready';
            this._statusLabel.label.text = `${phoneName} (${phoneIP}:${phonePort}) - Ready`;
            this._mountAction.visible = true;
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
        this.quickSettingsToggle = new QSyncQuickToggle(extension);
        this.quickSettingsItems.push(this.quickSettingsToggle);
    }
});

export default class QSyncExtension extends Extension {
    enable() {
        this._isActive = false;
        this._phoneIP = '';
        this._phonePort = 8484;
        this._daemonProc = null;
        this._pairDialog = null;

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
        this._checkInitialDaemon();
    }

    disable() {
        this._stopPolling();

        if (this._pairDialog) {
            this._pairDialog.close();
            this._pairDialog = null;
        }

        if (this._signalId && this._proxy) {
            this._proxy.disconnectSignal(this._signalId);
            this._signalId = null;
        }

        if (this._indicator) {
            this._indicator.destroy();
            this._indicator = null;
        }

        if (this._qsIndicator) {
            this._qsIndicator.destroy();
            this._qsIndicator = null;
        }

        this._proxy = null;
    }

    _buildPanelMenu() {
        this._menuHeader = new PopupMenu.PopupMenuItem('qSync: Ready', {reactive: false});
        this._indicator.menu.addMenuItem(this._menuHeader);

        this._menuSub = new PopupMenu.PopupMenuItem('Waiting...', {reactive: false});
        this._indicator.menu.addMenuItem(this._menuSub);

        this._indicator.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        this._actionPair = new PopupMenu.PopupMenuItem('🤝 Pair Phone...');
        this._actionPair.connect('activate', () => this.showPairDialog());
        this._indicator.menu.addMenuItem(this._actionPair);

        this._actionMount = new PopupMenu.PopupMenuItem('🔗 Mount Phone');
        this._actionMount.connect('activate', () => this.mountPhone());
        this._indicator.menu.addMenuItem(this._actionMount);

        this._actionOpen = new PopupMenu.PopupMenuItem('📂 Open in File Manager');
        this._actionOpen.connect('activate', () => this.openFileManager());
        this._indicator.menu.addMenuItem(this._actionOpen);

        this._actionUnmount = new PopupMenu.PopupMenuItem('⏏ Unmount Phone');
        this._actionUnmount.connect('activate', () => this.unmountPhone());
        this._indicator.menu.addMenuItem(this._actionUnmount);

        this._indicator.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        const turnOffItem = new PopupMenu.PopupMenuItem('⏹ Turn Off');
        turnOffItem.connect('activate', () => this.toggleActive(false));
        this._indicator.menu.addMenuItem(turnOffItem);
    }

    showPairDialog() {
        if (!this._pairDialog) {
            this._pairDialog = new QSyncPairDialog(this);
            this._pairDialog.connect('closed', () => {
                this._pairDialog = null;
            });
        }
        this._pairDialog.open();
    }

    pairWithPhone(pin, callback) {
        if (!this._proxy) {
            callback(false, '', 'Daemon not running. Turn on qSync first.');
            return;
        }
        const ip = this._phoneIP || '';
        this._proxy.PairRemote(pin, ip, 8484, (result, err) => {
            if (err || !result) {
                callback(false, '', err ? err.message : 'Unknown error');
                return;
            }
            const [success, serverName, errorMsg] = result;
            if (success) {
                this._refreshStatus();
            }
            callback(success, serverName, errorMsg);
        });
    }

    toggleActive(active) {
        this._isActive = active;

        if (active) {
            this._indicator.visible = true;
            this._startDaemon();
            this._startPolling();
        } else {
            this._indicator.visible = false;
            this._stopDaemon();
            this._stopPolling();
            this._updateUIState(false, '', '', 0, false);
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

        this._proxy.GetStatusRemote((result, error) => {
            if (!error && result) {
                this._isActive = true;
                this._indicator.visible = true;
                this._startPolling();
                const [phoneFound, phoneName, phoneIP, phonePort, isMounted] = result;
                this._updateUIState(phoneFound, phoneName, phoneIP, phonePort, isMounted);
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
                    if (error) return;
                    this._signalId = proxy.connectSignal('StatusChanged', () => {
                        this._refreshStatus();
                    });
                    this._checkInitialDaemon();
                }
            );
        } catch (e) {}
    }

    _refreshStatus() {
        if (!this._isActive || !this._proxy) return;

        this._proxy.GetStatusRemote((result, error) => {
            if (error || !result) {
                this._updateUIState(false, 'Daemon Offline', '', 0, false);
                return;
            }
            const [phoneFound, phoneName, phoneIP, phonePort, isMounted] = result;
            this._updateUIState(phoneFound, phoneName, phoneIP, phonePort, isMounted);
        });
    }

    _updateUIState(phoneFound, phoneName, phoneIP, phonePort, isMounted) {
        if (phoneIP) {
            this._phoneIP = phoneIP;
            this._phonePort = phonePort || 8484;
        }

        // 1. Update Quick Settings Toggle
        if (this._qsIndicator && this._qsIndicator.quickSettingsToggle) {
            this._qsIndicator.quickSettingsToggle.updateState(
                this._isActive,
                phoneFound,
                phoneName,
                phoneIP,
                phonePort,
                isMounted
            );
        }

        // 2. Update Top Bar Panel Indicator
        if (!this._icon || !this._menuHeader) return;

        this._icon.style_class = 'system-status-icon';
        if (isMounted) {
            this._icon.icon_name = 'drive-harddisk-symbolic';
            this._icon.add_style_class_name('qsync-indicator-mounted');
        } else if (phoneFound) {
            this._icon.icon_name = 'phone-symbolic';
            this._icon.add_style_class_name('qsync-indicator-online');
        } else {
            this._icon.icon_name = 'phone-symbolic';
            this._icon.add_style_class_name('qsync-indicator-offline');
        }

        if (phoneFound) {
            this._menuHeader.label.text = phoneName || 'Phone in Wi-Fi';
            this._menuSub.label.text = `${phoneIP}:${phonePort}` + (isMounted ? ' (Mounted)' : ' (Ready)');
        } else {
            this._menuHeader.label.text = phoneName || 'Phone Disconnected';
            this._menuSub.label.text = phoneIP ? `${phoneIP} (Offline)` : 'Searching on Wi-Fi...';
        }

        if (this._actionMount) this._actionMount.visible = (phoneFound && !isMounted);
        if (this._actionOpen) this._actionOpen.visible = isMounted;
        if (this._actionUnmount) this._actionUnmount.visible = isMounted;
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

    openFileManager() {
        if (!this._phoneIP) {
            this._phoneIP = '192.168.3.40';
        }
        const uri = `dav://qsync@${this._phoneIP}:${this._phonePort}/`;
        try {
            Gio.AppInfo.launch_default_for_uri(uri, null);
        } catch (e) {
            if (this._proxy) {
                this._proxy.OpenInNautilusRemote((result, err) => {});
            }
        }
    }
}
