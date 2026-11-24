// FireCortex desktop shell: window/tray/menu lifecycle, OAuth loopback, updater
// and session storage. No business logic — everything else is the apps/web build.

mod dialog;
mod distribution;
mod downloads;
#[cfg(target_os = "macos")]
mod menu;
mod oauth_loopback;
mod paths;
mod policy;
mod proxy;
mod session;
mod tabs;
mod tray;

use tauri::Manager;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // One TLS provider for both the updater and session refresh.
    session::ensure_tls_provider();

    // Before the builder: portable setup, WebView2 check, policy, and the
    // identifier a portable copy runs under (single-instance key + keychain).
    let mut context = tauri::generate_context!();
    let distribution = distribution::bootstrap(&mut context);
    let portable = distribution.portable.clone();

    let builder = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            tray::show_main_window(app);
        }))
        // Registered in every mode: `check_update_notice` uses it from Rust.
        // The webview only gets its permissions in `auto` mode (grant_updater).
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_notification::init())
        .manage(distribution)
        .manage(downloads::DownloadsState::default())
        .manage(oauth_loopback::LoopbackState::default())
        .manage(tabs::TabsState::default())
        .invoke_handler(tauri::generate_handler![
            distribution::get_distribution,
            distribution::check_update,
            distribution::check_update_notice,
            proxy::get_network_proxy,
            proxy::set_network_proxy,
            session::get_server,
            session::set_remote_server,
            session::leave_server,
            session::store_session,
            session::clear_session,
            session::refresh_session,
            oauth_loopback::start_oauth_loopback,
            oauth_loopback::stop_oauth_loopback,
            tabs::tabs_list,
            tabs::tabs_open,
            tabs::tabs_activate,
            tabs::tabs_close,
            tabs::tabs_move,
        ]);

    // macOS only: replaces Tauri's default menu, whose ⌘W hides the whole tab container.
    #[cfg(target_os = "macos")]
    let builder = builder
        .menu(menu::build)
        .on_menu_event(|app, event| menu::on_event(app, event.id().as_ref()));

    builder
        .setup(move |app| {
            // Plugins are set up first, so a second instance has already
            // handed over and exited by now.
            let paths = paths::AppPaths::resolve(app.handle(), portable.as_ref())?;
            app.manage(paths);
            // Before any webview exists: webviews take their proxy when created.
            let proxy_policy = app
                .state::<distribution::Distribution>()
                .policy
                .proxy
                .clone();
            app.manage(proxy::ProxyState::load(
                &paths::config_dir(app.handle()),
                &app.config().identifier,
                proxy_policy.as_ref(),
            ));
            distribution::grant_updater(app.handle())?;
            tabs::init(app.handle()).map_err(|e| e.0)?;
            #[cfg(desktop)]
            tray::create_tray(app.handle())?;
            Ok(())
        })
        .build(context)
        .expect("error while building FireCortex desktop")
        .run(|app, event| {
            #[cfg(target_os = "macos")]
            if let tauri::RunEvent::Reopen { .. } = event {
                tray::show_main_window(app);
            }
            #[cfg(not(target_os = "macos"))]
            let _ = (app, event);
        });
}
