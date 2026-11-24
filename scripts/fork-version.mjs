// Cargo, Tauri and npm require a three-component SemVer core.
const forkVersion = /^((?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*))\.([1-9]\d*)$/u;

export function packageVersion(version) {
  return version.replace(forkVersion, "$1-fc.$2");
}
