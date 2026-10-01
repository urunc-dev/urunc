#!/usr/bin/env bash

# Copyright (c) 2023-2026, Nubificus LTD
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o pipefail
set -o nounset

containerd_conf_file="/etc/containerd/config.toml"
containerd_conf_tmpl_file=""
# The templates k3s/rke2 render their configuration from, in the order they
# look for them
containerd_conf_tmpl_files=()
use_containerd_drop_in_conf_file="false"
# containerd 2.2 and newer ship with a default config that imports conf.d.
# Therefore, wherever possible, use this path for the drop-in files.
containerd_drop_in_conf_file="/etc/containerd/config.d/urunc-deploy.toml"
containerd_conf_d_import="/etc/containerd/conf.d/*.toml"
containerd_conf_d_drop_in_file="/etc/containerd/conf.d/urunc-deploy.toml"
# urunc's own containerd configuration file on k0s
containerd_k0s_conf_file="/etc/containerd/containerd.d/urunc.toml"
# The drop-in file, when the containerd configuration already imports a
# directory for drop-in files: conf.d, or the drop-in directory of k3s/rke2
containerd_imported_drop_in_file=""

# The node's container runtime version (e.g. containerd://2.0.5-k3s1)
# and its major version when the runtime is containerd, queried
# once and stored in these variables
container_runtime_version=""
containerd_major_version=""

# urunc installation directories
urunc_install_dir="/opt/urunc"
urunc_bin_dir="${urunc_install_dir}/bin"
urunc_share_dir="${urunc_install_dir}/share"
urunc_libexec_dir="${urunc_install_dir}/libexec"
urunc_config_dir="/etc/urunc"
urunc_config_file="${urunc_config_dir}/config.toml"
# Directories created by install outside of the urunc directories, so that
# uninstall removes only those
urunc_created_dirs_file="${urunc_install_dir}/created-dirs"
# containerd configuration files in which install set the debug level for
# DEBUG, each with the level it replaced (empty if none), so that uninstall
# restores the administrator's level instead of removing it
urunc_debug_level_files="${urunc_install_dir}/containerd-debug-level-files"

# Configurable keys discovered from config.toml
urunc_config_keys=""

HYPERVISORS="${HYPERVISORS:-"firecracker cloud-hypervisor qemu solo5-hvt solo5-spt"}"
IFS=' ' read -a hypervisors <<< "$HYPERVISORS"

function host_systemctl() {
    nsenter --target 1 --mount systemctl "${@}"
}

function print_usage() {
    echo "Usage: $0 {install|cleanup|reset}"
}

function install_artifact() {
    local src="$1"
    local dest="$2"
    cp "$src" "$dest"
    chmod +x "$dest"
}

function install_artifacts() {
    echo "copying urunc artifacts onto host"
    mkdir -p /host/usr/local/bin
    mkdir -p /host${urunc_bin_dir}
    mkdir -p /host${urunc_share_dir}
    mkdir -p /host${urunc_libexec_dir}

    install_artifact /urunc-artifacts/urunc /host/usr/local/bin/urunc
    install_artifact /urunc-artifacts/containerd-shim-urunc-v2 /host/usr/local/bin/containerd-shim-urunc-v2

    # install only the hypervisors found in the HYPERVISORS environment variable
    echo "Installing hypervisors: ${HYPERVISORS}"
    for hypervisor in "${hypervisors[@]}" ; do
        case "$hypervisor" in
        qemu)
            echo "Installing qemu"
            if which "qemu-system-$(uname -m)" >/dev/null 2>&1; then
                echo "QEMU is already installed."
            else
                install_artifact /urunc-artifacts/hypervisors/qemu-system-$(uname -m) /host${urunc_bin_dir}/qemu-system-$(uname -m)
                install_artifact /urunc-artifacts/libexec/virtiofsd /host${urunc_libexec_dir}/virtiofsd
                cp -r /urunc-artifacts/opt/kata/share/kata-qemu/qemu /host${urunc_share_dir}/
            fi
            ;;
        firecracker)
            echo "Installing firecracker"
            install_artifact /urunc-artifacts/hypervisors/firecracker /host${urunc_bin_dir}/firecracker
            ;;
        cloud-hypervisor)
            echo "Installing cloud-hypervisor"
            install_artifact /urunc-artifacts/hypervisors/cloud-hypervisor /host${urunc_bin_dir}/cloud-hypervisor
            ;;
        solo5-spt)
            echo "Installing solo5-spt"
            install_artifact /urunc-artifacts/hypervisors/solo5-spt /host${urunc_bin_dir}/solo5-spt
            ;;
        solo5-hvt)
            echo "Installing solo5-hvt"
            install_artifact /urunc-artifacts/hypervisors/solo5-hvt /host${urunc_bin_dir}/solo5-hvt
            ;;
        *)
            echo "Unsupported hypervisor: $hypervisor"
            ;;
        esac
    done
}

# Largest value accepted for integer configuration fields. TOML integers are
# signed 64-bit, and urunc discards the whole config.toml (falling back to its
# defaults) if any value fails to decode. The values are also read back from
# the container state with strconv.Atoi, which has the same limit.
URUNC_INT_MAX="9223372036854775807"

# Validate a single override value against its type. On failure it prints an
# ERROR describing the problem and returns non-zero; str values are unconstrained.
function validate_config_value() {
    local name="$1"
    local type="$2"
    local value="$3"
    case "${type}" in
        int)
	    # Positive, no zeros. Compared to URUNC_INT_MAX as strings, length
	    # first, to avoid shell overflow.
            if ! [[ "${value}" =~ ^[1-9][0-9]*$ ]] \
                || [ "${#value}" -gt "${#URUNC_INT_MAX}" ] \
                || { [ "${#value}" -eq "${#URUNC_INT_MAX}" ] && [[ "${value}" > "${URUNC_INT_MAX}" ]]; }; then
                echo "ERROR: invalid integer value for ${name} (expected 1-${URUNC_INT_MAX}): '${value}'" >&2
                return 1
            fi
            ;;
        bool)
            if [ "${value}" != "true" ] && [ "${value}" != "false" ]; then
                echo "ERROR: invalid boolean value for ${name} (expected 'true' or 'false'): '${value}'" >&2
                return 1
            fi
            ;;
    esac
    return 0
}

# Discover the configurable keys directly from the config file given as $1: every
# scalar leaf is an overridable variable. Emits one "ENV_VAR|jq path|type" line
# per leaf, so the env-var-to-config-key mapping is derived from config.toml
# rather than maintained by hand. The environment variable name is the upper-cased
# key path with '.' and '-' replaced by '_' and prefixed with URUNC_; the type is
# inferred from the value (number -> int, boolean -> bool, string -> str).
function urunc_config_specs() {
    local source_config="$1"
    # The $-variables below are jq variables, not shell variables, so the filter
    # is intentionally single-quoted.
    tomlq -r '
        paths(type == "string" or type == "number" or type == "boolean") as $p
        | ($p | map(ascii_upcase | gsub("-"; "_")) | join("_")) as $env
        | ($p | map("[\"" + . + "\"]") | join("")) as $path
        | ({"number": "int", "boolean": "bool", "string": "str"}[getpath($p) | type]) as $type
        | "URUNC_\($env)|.\($path)|\($type)"
    ' "${source_config}"
}

# Validate the URUNC_* configuration override variables If any override is
# invalid the installation is aborted, so it fails cleanly without leaving
# residual urunc binaries or configuration behind. All problems are reported
# together. The set of valid monitors is read from the config file shipped in
# the image so it stays in sync with the defaults.
function validate_urunc_config_env() {
    local source_config="$1"
    local errors=0

    # Build a map from recognized variable name -> type from the discovered
    # config specs. Its keys double as the set of recognized variable names.
    local -A types=()
    local name path type
    urunc_config_keys="$(urunc_config_specs "${source_config}")" \
        || die "failed to discover urunc configuration keys from ${source_config}"
    while IFS='|' read -r name path type; do
        types["${name}"]="${type}"
    done <<< "${urunc_config_keys}"

    # Flag any URUNC_* variable that is set, even if empty, but not recognized
    # (e.g. a typo in a monitor name), which would otherwise be silently ignored.
    while IFS= read -r name; do
        [ -z "${name}" ] && continue
        if [ -z "${types[$name]:-}" ]; then
            echo "ERROR: unknown urunc configuration variable: ${name}" >&2
            errors=$((errors + 1))
        fi
    done < <(compgen -e | grep '^URUNC_' || true)

    # Validate each recognized variable that is set against its declared type.
    local val
    for name in "${!types[@]}"; do
        val="${!name:-}"
        [ -z "${val}" ] && continue
        validate_config_value "${name}" "${types[$name]}" "${val}" || errors=$((errors + 1))
    done

    if [ "${errors}" -ne 0 ]; then
        die "${errors} invalid urunc configuration override(s); aborting before any changes are made to the host"
    fi
}

# Override values in the urunc configuration file from URUNC_* environment
# variables. Each variable maps to a key in config.toml: its name is the
# upper-cased TOML path with '.' and '-' replaced by '_', prefixed with URUNC_
# (e.g. monitors.cloud-hypervisor.default_memory_mb ->
# URUNC_MONITORS_CLOUD_HYPERVISOR_DEFAULT_MEMORY_MB). Unset or empty variables
# leave the value shipped in config.toml untouched. Values are expected to have
# already been validated.
function override_urunc_config_from_env() {
    local file="$1"

    # Build a single jq program that sets every overridden key.
    local filter="" name path type val expr
    [ -n "${urunc_config_keys}" ] \
        || die "urunc configuration keys not discovered; run validate_urunc_config_env first"
    while IFS='|' read -r name path type; do
        val="${!name:-}"
        [ -z "${val}" ] && continue
        case "${type}" in
            int|bool) expr="(\$ENV[\"${name}\"] | fromjson)" ;;
            *)        expr="\$ENV[\"${name}\"]" ;;
        esac
        if [ -n "${filter}" ]; then
            filter="${filter} | ${path} = ${expr}"
        else
            filter="${path} = ${expr}"
        fi
    done <<< "${urunc_config_keys}"

    # With no overrides, leave the shipped config.toml untouched (pristine).
    if [ -z "${filter}" ]; then
        echo "No urunc configuration overrides set; keeping the shipped defaults"
        return 0
    fi

    echo "Applying urunc configuration overrides from environment variables"

    # tomlq rewrites the whole file and drops comments, so capture the leading
    # comment block first and restore it afterward.
    local header
    header="$(awk '/^#/{print; next} {exit}' "${file}")"

    tomlq -i -t "${filter}" "${file}"

    if [ -n "${header}" ]; then
        local tmp="${file}.tmp"
        { printf '%s\n\n' "${header}"; cat "${file}"; } > "${tmp}" && mv "${tmp}" "${file}"
    fi

    return 0
}

function install_urunc_config() {
    echo "Installing urunc configuration file"
    mkdir -p /host${urunc_config_dir}
    sed "s|qemu-system-x86_64|qemu-system-$(uname -m)|" /deployment/config.toml > /host${urunc_config_file}
    override_urunc_config_from_env "/host${urunc_config_file}"
    echo "urunc configuration file installed at ${urunc_config_file}"
}

function remove_artifacts() {
    # Remove urunc related artifacts
    rm -f /host/usr/local/bin/urunc
    rm -f /host/usr/local/bin/containerd-shim-urunc-v2

    rm -rf /host${urunc_install_dir}
    rm -rf /host${urunc_config_dir}
    }


die() {
    msg="$*"
    echo "ERROR: $msg" >&2
    exit 1
}

function get_container_runtime() {
    local runtime="$container_runtime_version"

    if echo "$runtime" | grep -qE "cri-o"; then
        echo "crio"
    elif echo "$runtime" | grep -qE 'containerd.*-k3s'; then
        if host_systemctl is-active --quiet rke2-agent; then
            echo "rke2-agent"
        elif host_systemctl is-active --quiet rke2-server; then
            echo "rke2-server"
        elif host_systemctl is-active --quiet k3s-agent; then
            echo "k3s-agent"
        else
            echo "k3s"
        fi
    elif host_systemctl is-active --quiet k0scontroller; then
        echo "k0s-controller"
    elif host_systemctl is-active --quiet k0sworker; then
        echo "k0s-worker"
    else
        echo "$runtime" | awk -F '[:]' '{print $1}'
    fi
}

function is_containerd_capable_of_using_drop_in_files() {
    local runtime="$1"

    if [[ "$runtime" =~ ^(k0s-worker|k0s-controller)$ ]]; then
        # k0s does the work of using drop-in files better than any other "k8s distro", so
        # we don't mess up with what's being correctly done.
        echo "false"
        return
    fi

    if [ "${containerd_major_version:-0}" -lt 2 ]; then
        # Only containerd 2.0 does the merge of the plugins section from different snippets,
        # instead of overwriting the whole section, which makes things considerably more
        # complicated for us to deal with.
        #
        # It's been discussed with containerd community, and the patch needed will **NOT** be
        # backported to the release 1.7, as that breaks the behaviour from an existing release.
        echo "false"
        return
    fi

    echo "true"
}


function wait_till_node_is_ready() {
    local ready="False"

    while ! [[ "${ready}" == "True" ]]; do
        sleep 2s
        # Tolerate transient API server errors while the runtime restarts
        ready=$(kubectl get node $NODE_NAME -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}') || true
    done
}

function configure_cri_runtime() {
    configure_containerd "$1"
    if [ "$1" == "k0s-worker" ] || [ "$1" == "k0s-controller" ]; then
        # do nothing, k0s will automatically load the config on the fly
        :
    else
        echo "reloading $1"
        host_systemctl daemon-reload
        host_systemctl restart "$1"
    fi

    wait_till_node_is_ready
}

# Path of the record that urunc-deploy created the given containerd
# configuration file. Once install has configured the file, the marker holds
# its checksum, so that uninstall can tell whether it was changed since.
function created_marker_file() {
    echo "${1}.urunc-deploy-created"
}

function conf_file_checksum() {
    sha256sum "$1" | cut -d ' ' -f 1
}

# Check whether urunc-deploy created the given containerd configuration file
# and nobody changed it since. An empty marker means that install stopped
# before it stored the checksum.
function conf_file_unchanged_since_created() {
    local file="$1"
    local created_marker
    created_marker="$(created_marker_file "$file")"

    [ -f "$created_marker" ] && [ -f "$file" ] \
        && { [ ! -s "$created_marker" ] \
            || [ "$(conf_file_checksum "$file")" == "$(cat "$created_marker")" ]; }
}

# Create a directory along with its missing parents, recording each one that
# did not exist, so that uninstall removes only the directories it created.
function create_dirs() {
    local dir="$1"
    local missing=()

    while [ ! -d "$dir" ]; do
        missing=("$dir" "${missing[@]}")
        dir="$(dirname "$dir")"
    done
    [ ${#missing[@]} -eq 0 ] && return 0
    mkdir -p /host${urunc_install_dir}
    for dir in "${missing[@]}"; do
        mkdir "$dir"
        echo "$dir" >> /host${urunc_created_dirs_file}
    done
}

# Remove the directories recorded by create_dirs, deepest first, as long as
# they are empty.
function remove_created_dirs() {
    local dir

    [ -f /host${urunc_created_dirs_file} ] || return 0
    sort -r -u /host${urunc_created_dirs_file} | while IFS= read -r dir; do
        rmdir "$dir" 2>/dev/null || true
    done
}

# Create a containerd configuration file that did not exist before, with the
# content read from standard input. The marker is written first, so that a
# failure in between never leaves an unmarked file behind, which a later
# install would take for the administrator's own.
function create_containerd_conf_file() {
    local file="$1"

    create_dirs "$(dirname "$file")"
    : > "$(created_marker_file "$file")"
    cat > "$file"
}

# Print the CRI plugin ID matching the schema version of the containerd
# configuration, or the containerd version if the configuration has none.
# Every schema from version 3 on (e.g. version 4 in containerd 2.3) uses the
# split CRI plugins.
function get_containerd_pluginid() {
    local containerd_root_conf_file="$containerd_conf_file"
    if [[ "$1" =~ ^(k0s-worker|k0s-controller)$ ]]; then
        containerd_root_conf_file="/etc/containerd/containerd.toml"
    fi

    local config_version=""
    if [ -f "$containerd_root_conf_file" ]; then
        config_version=$(tomlq -r '.version // empty' "$containerd_root_conf_file") \
            || die "cannot parse the containerd configuration ${containerd_root_conf_file} as TOML"
    fi
    if [ "${config_version:-1}" -ge 3 ]; then
        echo \"io.containerd.cri.v1.runtime\"
    elif [ "${config_version:-1}" -eq 2 ]; then
        echo \"io.containerd.grpc.v1.cri\"
    elif [ "${containerd_major_version:-0}" -ge 2 ]; then
        # containerd 2.x reads a configuration without a version as version 1,
        # but only the split CRI plugin ID registers the runtime on every 2.x
        # release (2.0 to 2.2 ignore the cri one)
        echo \"io.containerd.cri.v1.runtime\"
    else
        echo cri
    fi
}

# Print the drop-in file to use when the containerd configuration already
# imports a directory for drop-in files, so that the configuration itself is
# not edited, or nothing otherwise: conf.d, or on k3s/rke2 (runtime given as
# $1) the drop-in directory of the configuration they render (config-v3.toml.d,
# or config.toml.d for version 2 templates). That import holds the path on the
# host, so it is only used if it is inside the directory mounted at
# /etc/containerd, where the drop-in file is written and later removed.
function imported_drop_in_file() {
    local imports import import_dir
    local k3s_drop_in_import='/config(-v3)?\.toml\.d/\*\.toml$'
    [ -f "$containerd_conf_file" ] || return 0
    imports=$(tomlq -r '(.imports // [])[]' "$containerd_conf_file") || return 0
    if grep -qxF "$containerd_conf_d_import" <<< "$imports"; then
        echo "/host${containerd_conf_d_drop_in_file}"
        return 0
    fi
    [[ "$1" =~ ^(k3s|k3s-agent|rke2-agent|rke2-server)$ ]] || return 0
    while IFS= read -r import; do
        [[ "$import" =~ $k3s_drop_in_import ]] || continue
        import_dir="$(dirname "$import")"
        # The same directory on the host, seen through the / mount
        if [ "/host$(dirname "$import_dir")" -ef "$(dirname "$containerd_conf_file")" ]; then
            echo "$(dirname "$containerd_conf_file")/$(basename "$import_dir")/urunc-deploy.toml"
            return 0
        fi
    done <<< "$imports"
}

# Check whether a containerd configuration that install may have edited still
# imports the config.d drop-in file: the main one, or on k3s/rke2 their
# templates (the configuration they render follows them on the next start).
# A configuration that holds the path but cannot be parsed may still import it.
function config_d_drop_in_still_imported() {
    local conf_file conf_files=("$containerd_conf_file")
    if [ -n "$containerd_conf_tmpl_file" ]; then
        conf_files=("${containerd_conf_tmpl_files[@]}")
    fi
    for conf_file in "${conf_files[@]}"; do
        # A file without the path cannot import it, even if it does not parse
        # (e.g. a template with Go template syntax install never edited)
        grep -qsF "${containerd_drop_in_conf_file}" "$conf_file" || continue
        tomlq -e --arg p "${containerd_drop_in_conf_file}" '(.imports // []) | index($p) != null' \
            "$conf_file" >/dev/null 2>&1 && return 0
        tomlq . "$conf_file" >/dev/null 2>&1 || return 0
    done
    return 1
}

# Remove the drop-in files that any install may have used, except the one
# given: the config.d and conf.d ones and, on k3s/rke2, the ones in their own
# drop-in directories. The config.d one is imported by its exact path, and
# containerd does not start if such an import is missing, so while a
# configuration still imports it (e.g. removing the import failed) it is only
# emptied.
function remove_drop_in_files() {
    local keep="${1:-}" file
    local files=(/host${containerd_conf_d_drop_in_file})
    if [ -n "$containerd_conf_tmpl_file" ]; then
        files+=("$(dirname "$containerd_conf_tmpl_file")"/config{-v3,}.toml.d/urunc-deploy.toml)
    fi
    for file in "${files[@]}"; do
        [ "$file" == "$keep" ] || rm -f "$file"
    done

    file=/host${containerd_drop_in_conf_file}
    if [ "$file" == "$keep" ] || [ ! -e "$file" ]; then
        return 0
    fi
    if config_d_drop_in_still_imported; then
        echo "WARNING: $containerd_drop_in_conf_file is still imported by the containerd" \
            "configuration, so it is emptied instead of removed; remove the import and" \
            "then the file"
        : > "$file"
    else
        rm -f "$file"
    fi
}

# TODO: edit containerd configurations with tomlkit, which is already in the
# image as a yq dependency, instead of tomlq. tomlq round-trips the file
# through JSON and drops any comments and formatting.
function configure_containerd() {
    # Configure containerd to use urunc:
    echo "Add urunc as a supported runtime for containerd"
    echo "Containerd conf file: $containerd_conf_file"

    # Checked before any change, since install may run again after the
    # administrator edited the file it created (e.g. after a node reboot)
    local unchanged_since_created="false"
    if conf_file_unchanged_since_created "$containerd_conf_file"; then
        unchanged_since_created="true"
    fi

    local urunc_runtime="urunc"
    local pluginid
    local configuration_file="${containerd_conf_file}"

    # Properly set the configuration file in case drop-in files are supported
    if [ $use_containerd_drop_in_conf_file = "true" ]; then
        if [ -n "$containerd_imported_drop_in_file" ]; then
            configuration_file="$containerd_imported_drop_in_file"
            # An earlier install may have used config.d: remove its import
            # and any urunc runtime left in the configuration, which is not
            # edited if it has neither. On k3s/rke2 they are in the template,
            # which main cleans up, not in the configuration they render.
            if [ -z "$containerd_conf_tmpl_file" ]; then
                remove_urunc_containerd_conf "${containerd_conf_file}"
            fi
        else
            configuration_file="/host${containerd_drop_in_conf_file}"
            if [ -z "$(tomlq -r --arg p "${containerd_drop_in_conf_file}" \
                '(.imports // []) | index($p) // empty' "${containerd_conf_file}")" ]; then
                tomlq -i -t --arg p "${containerd_drop_in_conf_file}" '.imports |= ((. // []) + [$p])' "${containerd_conf_file}"
            fi
        fi
        # Whichever other drop-in file an earlier install used, before the
        # configuration started or stopped importing a drop-in directory
        remove_drop_in_files "$configuration_file"
    fi

    pluginid=$(get_containerd_pluginid "$1")
    echo "Plugin ID: ${pluginid}"

    local runtime_table=".plugins.${pluginid}.containerd.runtimes.\"urunc\""
    local runtime_type=\"io.containerd.urunc.v2\"

    echo "Once again, configuration file is ${configuration_file}"

    create_dirs "$(dirname "${configuration_file}")"
    if [ $use_containerd_drop_in_conf_file = "true" ]; then
        # The drop-in file belongs to urunc-deploy, so start it empty, in case
        # an older one was left behind (e.g. with an outdated plugin ID)
        : > ${configuration_file}
    else
        touch ${configuration_file}
    fi

    # The urunc runtime and, for DEBUG, the containerd debug level are written
    # in a single pass
    local filter="${runtime_table}.runtime_type = ${runtime_type}"
    filter="${filter} | ${runtime_table}.container_annotations = [\"com.urunc.unikernel.*\"]"
    local debug_level=""
    if [ "${DEBUG}" == "true" ]; then
        # The drop-in file was just emptied, so only the configuration itself
        # can already have a level
        if [ $use_containerd_drop_in_conf_file != "true" ]; then
            debug_level="$(tomlq -r '.debug.level // empty' ${configuration_file})"
        fi
        if [ "$debug_level" != "debug" ]; then
            filter="${filter} | .debug.level = \"debug\""
            # The drop-in file is deleted as a whole on uninstall, so only a
            # level set in the configuration itself needs to be recorded
            if [ $use_containerd_drop_in_conf_file != "true" ]; then
                mkdir -p /host${urunc_install_dir}
                printf '%s\t%s\n' "${configuration_file}" "${debug_level}" \
                    >> /host${urunc_debug_level_files}
            fi
        fi
    fi
    tomlq -i -t "${filter}" ${configuration_file}

    # Record what a configuration install created looks like now, so that
    # uninstall removes it only if nobody changed it since. A marker of a
    # file changed since is left as is, so that uninstall keeps the file.
    if [ "$unchanged_since_created" == "true" ]; then
        conf_file_checksum "$containerd_conf_file" \
            > "$(created_marker_file "$containerd_conf_file")"
    fi
}

function cleanup_cri_runtime() {
    cleanup_containerd "${@:2}"
}

# Remove what urunc-deploy added to the given containerd configuration file:
# the urunc runtime table, found under whichever CRI plugin holds it (the file
# may have been migrated to a newer schema since install), along with the
# tables on its path left empty, the containerd debug level set for DEBUG, and
# the drop-in import. Files with none of these are left untouched.
function remove_urunc_containerd_conf() {
    local file="$1"
    local filter="."
    # The $-variables below are jq variables, not shell variables, so the
    # filters are intentionally single-quoted.
    local urunc_runtime_paths='paths | select(length == 5 and .[0] == "plugins"
        and .[2:] == ["containerd", "runtimes", "urunc"])'
    local found has_runtime has_import debug_level

    # A quick text search first, so that files without any mention of urunc,
    # even ones that do not parse, are not read as TOML at all
    if ! grep -qF -e "io.containerd.urunc.v2" -e "${containerd_drop_in_conf_file}" "$file"; then
        return 0
    fi
    # The text may only be in a comment or in another runtime, so check what
    # is actually there before rewriting the file, which drops its comments
    found=$(tomlq -r --arg p "${containerd_drop_in_conf_file}" \
        '"\([('"${urunc_runtime_paths}"')] | length > 0) \((.imports // []) | index($p) != null)"' \
        "$file") || return 1
    read -r has_runtime has_import <<< "$found"
    if [ "$has_runtime" != "true" ] && [ "$has_import" != "true" ]; then
        return 0
    fi

    if [ "$has_runtime" == "true" ]; then
        # The debug level was only set in the file holding the runtime, and
        # only if install recorded that it set it there. Restore the level it
        # replaced, as recorded by the latest install that changed it.
        if debug_level=$(awk -F '\t' -v f="$file" '$1 == f { l = $2; found = 1 }
                END { print l; exit !found }' /host${urunc_debug_level_files} 2>/dev/null); then
            filter="${filter}"' | if .debug.level == "debug" then
                (if $l == "" then del(.debug.level) else .debug.level = $l end) else . end'
            filter="${filter} | if .debug == {} then del(.debug) else . end"
        fi
        filter="${filter} | reduce (${urunc_runtime_paths})"' as $r (.;
            delpaths([$r])
            | reduce ($r[:-1], $r[:-2], $r[:-3], ["plugins"]) as $t (.;
                if getpath($t) == {} then delpaths([$t]) else . end))'
    fi
    filter="${filter} | if has(\"imports\") then .imports -= [\$p] | if .imports == [] then del(.imports) else . end else . end"

    tomlq -i -t --arg p "${containerd_drop_in_conf_file}" --arg l "${debug_level:-}" \
        "${filter}" "$file"
}

# Remove what urunc-deploy added to one containerd configuration file: the
# whole file if install created it and nobody changed it since, otherwise
# only the urunc entries.
function cleanup_containerd_conf_file() {
    local file="$1"
    local created_marker
    created_marker="$(created_marker_file "$file")"

    if conf_file_unchanged_since_created "$file"; then
        # An install created the configuration and nobody changed it since,
        # so remove it as a whole
        rm -f "$file" "$created_marker"
    elif [ -f "$file" ]; then
        local created="false"
        if [ -f "$created_marker" ]; then
            created="true"
            # Changed since install created it, so it is the administrator's now
            echo "Keeping $file: created by urunc-deploy but changed since," \
                "so only the urunc configuration is removed from it"
            if [ -n "$containerd_conf_tmpl_file" ]; then
                echo "k3s renders its containerd configuration from it until it is removed"
            fi
            rm -f "$created_marker"
        fi
        # Remove only what urunc-deploy added, so that changes made after the
        # installation are kept, whichever mode it used: a containerd upgrade
        # can change the mode between install and uninstall.
        remove_urunc_containerd_conf "$file" \
            || echo "WARNING: failed to remove the urunc configuration from $file"
        # A file left empty only ever held urunc's configuration, if
        # urunc-deploy created it. An administrator's file may have held only
        # comments, which tomlq drops, so it is kept even if empty.
        if { [ "$created" == "true" ] || [ "$file" == "$containerd_k0s_conf_file" ]; } \
            && ! grep -q '[^[:space:]]' "$file"; then
            rm -f "$file"
        fi
    else
        rm -f "$created_marker"
    fi
}

# Clean up the given containerd configuration files, then the drop-in files
# and the directories install created.
function cleanup_containerd() {
    local file
    for file in "$@"; do
        cleanup_containerd_conf_file "$file"
    done
    # Whichever drop-in file install used, since the configuration may have
    # started or stopped importing a drop-in directory since then
    remove_drop_in_files

    # Only after the files in them are gone
    remove_created_dirs
}

function restart_cri_runtime() {
    local runtime="${1}"

    if [ "${runtime}" == "k0s-worker" ] || [ "${runtime}" == "k0s-controller" ]; then
        # do nothing, k0s will automatically unload the config on the fly
        :
    else
        host_systemctl daemon-reload
        host_systemctl restart "${runtime}"
    fi
}

function reset_runtime() {
    restart_cri_runtime "$1"

    if [ "$1" == "containerd" ]; then
        host_systemctl restart kubelet
    fi

    wait_till_node_is_ready

    # Remove the label only as the very last step. The cleanup DaemonSet
    # selects nodes by this label, so removing it makes the DaemonSet
    # controller delete this very Pod. Retry on transient API errors rather
    # than exiting, which would restart the runtime again.
    until kubectl label node "$NODE_NAME" urunc.io/urunc-runtime-; do
        sleep 2s
    done
    echo "urunc-deploy uninstalled successfully"
}

function main() {
    action=${1:-}
    if [ -z "$action" ]; then
        print_usage
        die "invalid arguments"
    fi
    echo "Action:"
    echo "* $action"
    echo ""
    echo "Environment variables passed to this script"
    echo "* NODE_NAME: ${NODE_NAME}"
    echo "* HYPERVISORS: ${HYPERVISORS}"

    # verify user is root
    euid=$(id -u)
    if [[ $euid -ne 0 ]]; then
        die  "This script must be run as root"
    fi
    container_runtime_version=$(kubectl get node $NODE_NAME -o jsonpath='{.status.nodeInfo.containerRuntimeVersion}') \
        || die "invalid node name"
    containerd_major_version=$(echo "$container_runtime_version" | sed -n 's|.*containerd://v\{0,1\}\([0-9][0-9]*\).*|\1|p')
    if [[ "$container_runtime_version" == containerd://* ]] && [ -z "$containerd_major_version" ]; then
        die "cannot parse the containerd version '${container_runtime_version}'"
    fi
    runtime=$(get_container_runtime)
    if [[ "$runtime" =~ ^(k3s|k3s-agent|rke2-agent|rke2-server)$ ]]; then
        # TODO: rke2 keeps its containerd directory under
        # /var/lib/rancher/rke2/agent/etc/containerd, but only a k3s overlay
        # exists, which mounts the k3s one at /etc/containerd, so rke2 needs
        # its own overlay. Untested on rke2.
        # k3s renders its configuration from config-v3.toml.tmpl if it exists,
        # and from config.toml.tmpl only otherwise. A missing template is
        # created as config-v3.toml.tmpl with containerd 2.x, whose rendered
        # (version 3) configuration it is a copy of.
        containerd_conf_tmpl_files=("$(dirname "$containerd_conf_file")"/config{-v3,}.toml.tmpl)
        containerd_conf_tmpl_file="${containerd_conf_tmpl_files[1]}"
        if [ -f "${containerd_conf_tmpl_files[0]}" ] \
            || { [ ! -f "$containerd_conf_tmpl_file" ] && [ "${containerd_major_version:-0}" -ge 2 ]; }; then
            containerd_conf_tmpl_file="${containerd_conf_tmpl_files[0]}"
        fi
    elif [[ "$runtime" =~ ^(k0s-worker|k0s-controller)$ ]]; then
        # From 1.27.1 onwards k0s enables dynamic configuration on containerd CRI runtimes.
        # This works by k0s creating a special directory in /etc/k0s/containerd.d/ where user can drop-in partial containerd configuration snippets.
        # k0s will automatically pick up these files and adds these in containerd configuration imports list.
        containerd_conf_file="${containerd_k0s_conf_file}"
    fi

    use_containerd_drop_in_conf_file=$(is_containerd_capable_of_using_drop_in_files "$runtime")

    echo "Runtime: ${runtime}"
    echo "containerd_conf_file: ${containerd_conf_file}"
    echo "containerd_conf_tmpl_file: ${containerd_conf_tmpl_file}"
    echo "Using containerd drop-in files: $use_containerd_drop_in_conf_file"

    # Validate the runtime before any action touches the host: install never
    # ran on an unsupported runtime, so cleanup must not label the node and
    # reset must not restart the runtime.
    # TODO: support CRI-O nodes (detected as "crio"): register urunc in the
    # CRI-O configuration on install, remove it on cleanup and restart crio
    # on reset
    if ! [[ "$runtime" =~ ^(containerd|k3s|k3s-agent|rke2-agent|rke2-server|k0s-controller|k0s-worker)$ ]]; then
        die "unsupported container runtime: ${runtime}"
    fi

    case "$action" in
        install)
            # Validate the configuration overrides before touching the host,
            # so an invalid value fails the installation without leaving
            # residual artifacts behind.
            validate_urunc_config_env "/deployment/config.toml"
            # k3s/rke2 always render their configuration, so without it or a
            # template the mount does not point to their directory, and a
            # template holding only urunc's runtime would replace all of it
            if [[ "$runtime" =~ ^(k3s|k3s-agent|rke2-agent|rke2-server)$ ]] \
                && [ ! -f "$containerd_conf_file" ] && [ ! -f "$containerd_conf_tmpl_file" ]; then
                die "no k3s/rke2 containerd configuration (config.toml or a template)" \
                    "found in $(dirname "$containerd_conf_file"); check that the k3s overlay" \
                    "mounts their containerd directory (e.g. for a custom --data-dir)"
            fi
            if [ "$use_containerd_drop_in_conf_file" == "true" ]; then
                containerd_imported_drop_in_file=$(imported_drop_in_file "$runtime")
            fi
            # The containerd configuration is edited as TOML, so it must parse.
            # The k3s/rke2 template is not read when the configuration already
            # imports a drop-in directory, so it may then use Go template syntax.
            local conf_file files_to_parse=("$containerd_conf_file")
            if [ -z "$containerd_imported_drop_in_file" ]; then
                files_to_parse+=("$containerd_conf_tmpl_file")
            fi
            for conf_file in "${files_to_parse[@]}"; do
                if [ -n "$conf_file" ] && [ -f "$conf_file" ] && ! tomlq . "$conf_file" >/dev/null; then
                    if [ "$conf_file" == "$containerd_conf_tmpl_file" ]; then
                        die "cannot parse the containerd configuration ${conf_file} as TOML;" \
                            "templates with Go template syntax are not supported"
                    fi
                    die "cannot parse the containerd configuration ${conf_file} as TOML"
                fi
            done
            if [[ "$runtime" =~ ^(k3s|k3s-agent|rke2-agent|rke2-server)$ ]]; then
                if [ -n "$containerd_imported_drop_in_file" ]; then
                    # The drop-in file goes into the directory the configuration
                    # imports, as Kata does, so no template is needed. Remove
                    # what an earlier install added to a template.
                    for conf_file in "${containerd_conf_tmpl_files[@]}"; do
                        cleanup_containerd_conf_file "$conf_file"
                    done
                else
                    # With containerd 1.x, where an imported file replaces whole
                    # plugin sections, or without that import, edit the template
                    # k3s/rke2 render the configuration from
                    if [ ! -f "$containerd_conf_tmpl_file" ] && [ -f "$containerd_conf_file" ]; then
                        create_containerd_conf_file "$containerd_conf_tmpl_file" < "$containerd_conf_file"
                    fi
                    # Only set the containerd_conf_file to its new value after
                    # copying the file to the template location
                    containerd_conf_file="${containerd_conf_tmpl_file}"
                fi
            elif [[ "$runtime" =~ ^(k0s-worker|k0s-controller)$ ]]; then
                if [ ! -f "$containerd_conf_file" ]; then
                    create_containerd_conf_file "$containerd_conf_file" < /dev/null
                fi
            elif [[ "$runtime" == "containerd" ]]; then
                if [ ! -f "$containerd_conf_file" ]; then
                    # containerd can run without a configuration file, but urunc
                    # needs one to register its runtime. Write just the schema
                    # version, which leaves containerd on its built-in defaults:
                    # 3 since containerd 2.0, 2 before it.
                    echo "No containerd configuration found, creating one"
                    local schema_version=2
                    if [ "${containerd_major_version:-0}" -ge 2 ]; then
                        schema_version=3
                    fi
                    echo "version = ${schema_version}" | create_containerd_conf_file "$containerd_conf_file"
                fi
            fi
            install_artifacts
            install_urunc_config
            configure_cri_runtime "$runtime"
            kubectl label node "$NODE_NAME" --overwrite urunc.io/urunc-runtime=true
            echo "urunc-deploy completed successfully"
        ;;
        cleanup)
            local conf_files=("$containerd_conf_file")
            if [[ "$runtime" =~ ^(k3s|k3s-agent|rke2-agent|rke2-server)$ ]]; then
                # Both templates, since config-v3.toml.tmpl may have been added
                # or removed since install
                conf_files=("${containerd_conf_tmpl_files[@]}")
            fi

            cleanup_cri_runtime "$runtime" "${conf_files[@]}"
            local urunc_deploy_installations=$(kubectl -n kube-system get ds | grep urunc-deploy | wc -l)
            if [ $urunc_deploy_installations -eq 0 ]; then
                kubectl label node "$NODE_NAME" --overwrite urunc.io/urunc-runtime=cleanup
            fi
            remove_artifacts
            ;;
        reset)
            reset_runtime $runtime
            ;;
        *)
            print_usage
            die "invalid arguments"
        ;;
    esac

    if [ "$action" != "cleanup" ]; then
        trap 'exit 0' TERM INT
        sleep infinity &
        wait $!
    fi
}

main "$@"
