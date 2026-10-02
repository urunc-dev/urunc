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
containerd_conf_file_backup="${containerd_conf_file}.bak"
containerd_conf_tmpl_file=""
use_containerd_drop_in_conf_file="false"
containerd_drop_in_conf_file="/etc/containerd/config.d/urunc-deploy.toml"

# urunc installation directories
urunc_install_dir="/opt/urunc"
urunc_bin_dir="${urunc_install_dir}/bin"
urunc_share_dir="${urunc_install_dir}/share"
urunc_libexec_dir="${urunc_install_dir}/libexec"
urunc_config_dir="/etc/urunc"
urunc_config_file="${urunc_config_dir}/config.toml"

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
    local runtime=$(kubectl get node $NODE_NAME -o jsonpath='{.status.nodeInfo.containerRuntimeVersion}')
    if [ "$?" -ne 0 ]; then
                die "invalid node name"
    fi

    if echo "$runtime" | grep -qE "cri-o"; then
        echo "cri-o"
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

    if [ "$runtime" == "crio" ]; then
        # This should never happen but better be safe than sorry
        echo "false"
        return
    fi

    if [[ "$runtime" =~ ^(k0s-worker|k0s-controller)$ ]]; then
        # k0s does the work of using drop-in files better than any other "k8s distro", so
        # we don't mess up with what's being correctly done.
        echo "false"
        return
    fi

    local version_major=$(kubectl get node $NODE_NAME -o jsonpath='{.status.nodeInfo.containerRuntimeVersion}' | sed -n 's|.*containerd://\([0-9][0-9]*\).*|\1|p')
    if [ $version_major -lt 2 ]; then
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
    case $1 in
        crio)
            # TODO: Configure crio
            die "crio is not supported"
        ;;
        containerd | k3s | k3s-agent | rke2-agent | rke2-server | k0s-controller | k0s-worker)
            configure_containerd "$1"
        ;;
    esac
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

function configure_containerd() {
    # Configure containerd to use urunc:
    echo "Add urunc as a supported runtime for containerd"
    echo "Containerd conf file: $containerd_conf_file"
    mkdir -p /etc/containerd/

    if [ $use_containerd_drop_in_conf_file = "false" ] && [ -f "$containerd_conf_file" ]; then
        # only backup in case drop-in files are not supported, and when doing the backup
        # only do it if a backup doesn't already exist (don't override original)
        cp -n "$containerd_conf_file" "$containerd_conf_file_backup"
    fi

    if [ $use_containerd_drop_in_conf_file = "true" ]; then
        if [ -z "$(tomlq -r --arg p "${containerd_drop_in_conf_file}" \
            '(.imports // []) | index($p) // empty' "${containerd_conf_file}")" ]; then
            tomlq -i -t --arg p "${containerd_drop_in_conf_file}" '.imports |= ((. // []) + [$p])' "${containerd_conf_file}"
        fi
    fi
    local urunc_runtime="urunc"
    local pluginid=cri
    local configuration_file="${containerd_conf_file}"

    # Properly set the configuration file in case drop-in files are supported
    if [ $use_containerd_drop_in_conf_file = "true" ]; then
        configuration_file="/host${containerd_drop_in_conf_file}"
    fi

    local containerd_root_conf_file="$containerd_conf_file"
    if [[ "$1" =~ ^(k0s-worker|k0s-controller)$ ]]; then
        containerd_root_conf_file="/etc/containerd/containerd.toml"
    fi

    if grep -q "version = 2\>" $containerd_root_conf_file; then
        pluginid=\"io.containerd.grpc.v1.cri\"
    fi

    if grep -q "version = 3\>" $containerd_root_conf_file; then
        pluginid=\"io.containerd.cri.v1.runtime\"
    fi

    echo "Plugin ID: ${pluginid}"

    local runtime_table=".plugins.${pluginid}.containerd.runtimes.\"urunc\""
    local runtime_type=\"io.containerd.urunc.v2\"

    echo "Once again, configuration file is ${configuration_file}"

    mkdir -p $(dirname ${configuration_file})
    touch ${configuration_file}

    tomlq -i -t $(printf '%s.runtime_type=%s' ${runtime_table} ${runtime_type}) ${configuration_file}
    tomlq -i -t $(printf '%s.container_annotations=["com.urunc.unikernel.*"]' ${runtime_table}) ${configuration_file}

    if [ "${DEBUG}" == "true" ]; then
        tomlq -i -t '.debug.level = "debug"' ${configuration_file}
    fi
}

function cleanup_cri_runtime() {
    case $1 in
    crio)
        # TODO: Cleanup crio
        die "crio is not supported"
        ;;
    containerd | k3s | k3s-agent | rke2-agent | rke2-server | k0s-controller | k0s-worker)
        cleanup_containerd
        ;;
    esac
}

function cleanup_containerd() {
    if [ $use_containerd_drop_in_conf_file = "true" ]; then
        # There's no need to remove the drop-in file, as it'll be removed as
        # part of the artefacts removal.  Thus, simply remove the file from
        # the imports line of the containerd configuration and return.
        tomlq -i -t $(printf '.imports|=.-["%s"]' ${containerd_drop_in_conf_file}) ${containerd_conf_file}
        return
    fi

    rm -f $containerd_conf_file
    if [ -f "$containerd_conf_file_backup" ]; then
        mv "$containerd_conf_file_backup" "$containerd_conf_file"
    fi
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

    if [ "$1" == "crio" ] || [ "$1" == "containerd" ]; then
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
    runtime=$(get_container_runtime)
    if [[ "$runtime" =~ ^(k3s|k3s-agent|rke2-agent|rke2-server)$ ]]; then
        containerd_conf_tmpl_file="${containerd_conf_file}.tmpl"
        containerd_conf_file_backup="${containerd_conf_tmpl_file}.bak"
    elif [[ "$runtime" =~ ^(k0s-worker|k0s-controller)$ ]]; then
        # From 1.27.1 onwards k0s enables dynamic configuration on containerd CRI runtimes.
        # This works by k0s creating a special directory in /etc/k0s/containerd.d/ where user can drop-in partial containerd configuration snippets.
        # k0s will automatically pick up these files and adds these in containerd configuration imports list.
        containerd_conf_file="/etc/containerd/containerd.d/urunc.toml"
        containerd_conf_file_backup="${containerd_conf_tmpl_file}.bak"
    fi

    use_containerd_drop_in_conf_file=$(is_containerd_capable_of_using_drop_in_files "$runtime")
    echo "Using containerd drop-in files: $use_containerd_drop_in_conf_file"

    echo "Runtime: ${runtime}"
    echo "containerd_conf_file: ${containerd_conf_file}"
    echo "containerd_conf_tmpl_file: ${containerd_conf_tmpl_file}"
    echo "containerd_conf_file_backup: ${containerd_conf_file_backup}"
    echo "Using containerd drop-in files: $use_containerd_drop_in_conf_file"

    case "$action" in
        install)
            # Validate configuration overrides before touching the host, so an
            # invalid value fails the installation without leaving residual
            # artifacts behind.
            validate_urunc_config_env "/deployment/config.toml"
            if [[ "$runtime" =~ ^(k3s|k3s-agent|rke2-agent|rke2-server)$ ]]; then
                if [ ! -f "$containerd_conf_tmpl_file" ] && [ -f "$containerd_conf_file" ]; then
                    cp "$containerd_conf_file" "$containerd_conf_tmpl_file"
                fi
                # Only set the containerd_conf_file to its new value after
                # copying the file to the template location
                containerd_conf_file="${containerd_conf_tmpl_file}"
                containerd_conf_file_backup="${containerd_conf_tmpl_file}.bak"
            elif [[ "$runtime" =~ ^(k0s-worker|k0s-controller)$ ]]; then
                mkdir -p $(dirname "$containerd_conf_file")
                touch "$containerd_conf_file"
            elif [[ "$runtime" == "containerd" ]]; then
                if [ ! -f "$containerd_conf_file" ] && [ -d $(dirname "$containerd_conf_file") ] && [ -x $(command -v containerd) ]; then
                    containerd config default > "$containerd_conf_file"
                fi
            fi
            install_artifacts
            install_urunc_config
            configure_cri_runtime "$runtime"
            kubectl label node "$NODE_NAME" --overwrite urunc.io/urunc-runtime=true
            echo "urunc-deploy completed successfully"
        ;;
        cleanup)
            if [[ "$runtime" =~ ^(k3s|k3s-agent|rke2-agent|rke2-server)$ ]]; then
                containerd_conf_file_backup="${containerd_conf_tmpl_file}.bak"
                containerd_conf_file="${containerd_conf_tmpl_file}"
            fi

            cleanup_cri_runtime "$runtime"
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
