#!/usr/bin/env bash
# Isolated installer regression tests. No root, real network, or systemd needed.
set -o pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1
source install-komari.sh
command -v jq >/dev/null || { printf 'jq is required\n' >&2; exit 1; }
test_base=$(pwd -P)
TEST_ROOT=$(mktemp -d "$test_base/.install-test.XXXXXX") || exit 1
# Verify the absolute workspace boundary before allowing recursive cleanup.
[[ "$TEST_ROOT" == "$test_base"/.install-test.* && -d "$TEST_ROOT" ]] || exit 1
trap 'rm -rf -- "$TEST_ROOT"' EXIT

log_info() { :; }
log_success() { :; }
log_error() { :; }
log_step() { :; }
print_download_progress() { :; }
get_remote_size() { printf '0\n'; }
sleep() { :; }
install_dependencies() { return 0; }
check_systemd() { return 0; }
show_access_info() { :; }
create_systemd_service() { :; }
select_channel() { CHANNEL=stable; CHANNEL_NAME=stable; }
ui_input() { printf '25774\n'; }
ui_msgbox() { printf '%s\n' "$2" >> "$TEST_ROOT/messages"; }
uname() { case "$1" in -s) printf 'Linux\n';; -m) printf 'x86_64\n';; esac; }

STABLE_JSON='{"tag_name":"1.7.0","draft":false,"prerelease":false,"assets":[{"name":"komari-linux-amd64","state":"uploaded"}]}'
SNAPSHOT_JSON='[{"tag_name":"Snapshot-2610081200","draft":true,"prerelease":true,"assets":[{"name":"komari-linux-amd64","state":"uploaded"}]},{"tag_name":"Snapshot-2610081100","draft":false,"prerelease":true,"assets":[]},{"tag_name":"Snapshot-2610081000","draft":false,"prerelease":true,"assets":[{"name":"komari-linux-amd64","state":"uploaded"}]}]'
curl() {
    local target="" arg
    while [ "$#" -gt 0 ]; do
        arg="$1"; shift
        if [ "$arg" = '-o' ]; then target="$1"; shift; fi
    done
    if [ -n "$target" ]; then
        printf '\177ELFNEW' > "$target"
    elif [ "$CHANNEL" = snapshot ]; then
        printf '%s\n' "$SNAPSHOT_JSON"
    else
        printf '%s\n' "$STABLE_JSON"
    fi
}
systemctl() {
    printf '%s\n' "$*" >> "$TEST_ROOT/systemctl"
    return 0
}

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
assert_equal() { [ "$1" = "$2" ] || fail "expected [$2], got [$1]"; }
setup_install() {
    INSTALL_DIR=$(mktemp -d "$TEST_ROOT/case.XXXXXX") || exit 1
    DATA_DIR="$INSTALL_DIR"
    BINARY_PATH="$INSTALL_DIR/komari"
    BACKUP_DIR="$INSTALL_DIR/backup"
    DATA_BACKUP_DIR="$INSTALL_DIR/data/backup"
    : > "$TEST_ROOT/systemctl"
    : > "$TEST_ROOT/messages"
}
setup_upgrade() {
    setup_install
    printf '\177ELFOLD' > "$BINARY_PATH"
    chmod +x "$BINARY_PATH"
    mkdir -p "$DATA_DIR/data"
    printf 'old database\n' > "$DATA_DIR/data/komari.db"
    printf 'historical binary\n' > "${BINARY_PATH}.backup.history"
}
assert_old_binary() { assert_equal "$(od -An -tx1 "$BINARY_PATH" | tr -d '[:space:]')" 7f454c464f4c44; }
assert_no_service_calls() { [ ! -s "$TEST_ROOT/systemctl" ] || fail 'service was changed'; }

test_stable_url() {
    assert_equal "$REPO" SIXOSN/komari
    assert_equal "$(get_download_url amd64)" 'https://github.com/SIXOSN/komari/releases/download/1.7.0/komari-linux-amd64'
}
test_snapshot_url() {
    CHANNEL=snapshot
    assert_equal "$(get_download_url amd64)" 'https://github.com/SIXOSN/komari/releases/download/Snapshot-2610081000/komari-linux-amd64'
}
test_missing_asset() { get_download_url arm64 >/dev/null 2>&1 && fail 'missing asset accepted'; return 0; }
test_invalid_json() { curl() { printf 'not JSON'; }; get_download_url amd64 >/dev/null 2>&1 && fail 'invalid JSON accepted'; return 0; }
test_no_snapshot() { CHANNEL=snapshot; SNAPSHOT_JSON='[]'; get_download_url amd64 >/dev/null 2>&1 && fail 'empty snapshots accepted'; return 0; }
test_architectures() {
    local pair hardware expected
    for pair in 'x86_64 amd64' 'aarch64 arm64' 'i686 386' 'riscv64 riscv64' 'loongarch64 loong64'; do
        read -r hardware expected <<< "$pair"
        uname() { case "$1" in -s) printf 'Linux\n';; -m) printf '%s\n' "$hardware";; esac; }
        assert_equal "$(detect_arch)" "$expected"
    done
    uname() { case "$1" in -s) printf 'Linux\n';; -m) printf 'armv7l\n';; esac; }
    detect_arch >/dev/null && fail 'unsupported architecture accepted'
    uname() { printf 'Darwin\n'; }
    detect_arch >/dev/null && fail 'non-Linux platform accepted'
    return 0
}
test_failed_download() {
    setup_upgrade
    curl() { return 22; }
    download_file invalid "$BINARY_PATH" test && fail 'failed download accepted'
    assert_old_binary
    download_file invalid "$INSTALL_DIR/missing" test && fail 'failed new download accepted'
    [ ! -e "$INSTALL_DIR/missing" ] || fail 'failed download left a target'
    local leftovers=("$INSTALL_DIR"/*.download.*)
    [ ! -e "${leftovers[0]}" ] || fail 'download temporary file leaked'
}
test_invalid_binary() {
    setup_upgrade
    curl() { while [ "$#" -gt 0 ]; do if [ "$1" = -o ]; then printf '<html>error</html>' > "$2"; return 0; fi; shift; done; }
    download_file invalid "$BINARY_PATH" test && fail 'HTML accepted as binary'
    assert_old_binary
}
test_successful_download() {
    setup_install
    download_file valid "$BINARY_PATH" test || fail 'valid ELF rejected'
    is_installed || fail 'valid download not recognized'
}
test_empty_binary() { setup_install; : > "$BINARY_PATH"; chmod +x "$BINARY_PATH"; is_installed && fail 'empty file treated as installed'; return 0; }
test_install_lookup_failure() {
    setup_install
    curl() { return 22; }
    install_binary && fail 'failed lookup reported successful installation'
    [ ! -e "$BINARY_PATH" ] || fail 'lookup failure created a binary'
    assert_no_service_calls
}
test_manual_install() {
    setup_install
    check_systemd() { return 1; }
    install_binary || fail 'manual installation failed'
    is_installed || fail 'manual installation missing binary'
    assert_no_service_calls
}
test_upgrade_lookup_failure() {
    setup_upgrade
    curl() { return 22; }
    upgrade_komari && fail 'lookup failure reported upgrade success'
    assert_old_binary
    assert_no_service_calls
}
test_upgrade_download_failure() {
    setup_upgrade
    curl() { case " $* " in *' -o '*) return 22;; *) printf '%s' "$STABLE_JSON";; esac; }
    upgrade_komari && fail 'download failure reported upgrade success'
    assert_old_binary
    assert_no_service_calls
}
test_upgrade_stop_failure() {
    setup_upgrade
    systemctl() { printf '%s\n' "$*" >> "$TEST_ROOT/systemctl"; [ "$1" != stop ]; }
    upgrade_komari && fail 'stop failure reported upgrade success'
    assert_old_binary
}
test_upgrade_backup_failure() {
    setup_upgrade
    tar() { return 1; }
    upgrade_komari && fail 'backup failure reported upgrade success'
    assert_old_binary
    grep -q '^start ' "$TEST_ROOT/systemctl" || fail 'original service not restarted'
}
test_upgrade_success() {
    setup_upgrade
    upgrade_komari || fail 'upgrade failed'
    assert_equal "$(od -An -tx1 "$BINARY_PATH" | tr -d '[:space:]')" 7f454c464e4557
    assert_equal "$(cat "$DATA_DIR/data/komari.db")" 'old database'
    [ -f "${BINARY_PATH}.backup.history" ] || fail 'historical backup deleted'
    local archives=("$BACKUP_DIR"/upgrade-data.*.tar.gz)
    [ -s "${archives[0]}" ] || fail 'local-data snapshot missing'
}
test_upgrade_rollback() {
    setup_upgrade
    systemctl() {
        printf '%s\n' "$*" >> "$TEST_ROOT/systemctl"
        local magic
        magic=$(od -An -tx1 "$BINARY_PATH" | tr -d '[:space:]')
        if [ "$1" = start ] && [ "$magic" = 7f454c464e4557 ]; then
            printf 'changed database\n' > "$DATA_DIR/data/komari.db"
        fi
        if [ "$1" = is-active ] && [ "$magic" = 7f454c464e4557 ]; then return 1; fi
        return 0
    }
    upgrade_komari && fail 'failed startup reported success'
    assert_old_binary
    assert_equal "$(cat "$DATA_DIR/data/komari.db")" 'old database'
    local failed_data=("$DATA_DIR"/data.failed.*)
    assert_equal "$(cat "${failed_data[0]}/komari.db")" 'changed database'
    grep -q 'previous binary and local data were restored\|已恢复旧程序及本地数据' "$TEST_ROOT/messages" || fail 'recovery not reported'
}

passed=0 failed=0
for test_function in test_stable_url test_snapshot_url test_missing_asset test_invalid_json test_no_snapshot \
    test_architectures test_failed_download test_invalid_binary test_successful_download test_empty_binary \
    test_install_lookup_failure test_manual_install test_upgrade_lookup_failure test_upgrade_download_failure \
    test_upgrade_stop_failure test_upgrade_backup_failure test_upgrade_success test_upgrade_rollback; do
    if ("$test_function"); then
        printf 'PASS %s\n' "$test_function"
        passed=$((passed + 1))
    else
        failed=$((failed + 1))
    fi
done
printf '%s passed, %s failed\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
