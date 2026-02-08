# FAQs and Troubleshooting

This document collects frequently asked questions and common troubleshooting
tips for users working with urunc. It is intended to help new users quickly
understand common pitfalls and resolve issues without needing to search through
issues or source code.

---

## Frequently Asked Questions

### What is urunc and how is it different from runc?

urunc is a container runtime designed specifically for running unikernels.
Unlike runc, which targets standard Linux containers, urunc focuses on
booting lightweight, single-purpose unikernel images using different
hypervisors.

---

### Which unikernel frameworks are supported?

urunc supports multiple unikernel frameworks. The list of supported frameworks
and their capabilities can be the found in the [unikernel support documentation](unikernel-support.md)

Support may vary depending on the hypervisor and guest configuration.

---

### Why does my unikernel fail to start?

A unikernel may fail to start for several reasons, including:
- Missing or misconfigured hypervisor
- Invalid unikernel image
- Incorrect urunc configuration
- Missing permissions or required system capabilities

Checking logs and configuration files usually provides more details.

---

### Where can I find logs when something goes wrong?

Logs are typically printed to standard output or error when running urunc.
Depending on the setup, additional logs may be available from the hypervisor
or container runtime integration being used.

---

### Is Docker required to use urunc?

Docker is not required to run urunc, but it can be useful for building
reproducible unikernel images and ensuring consistent build environments.

---

## Troubleshooting

### Installation issues

**Problem:** `urunc: command not found`

**Possible causes:**
- The urunc binary is not installed
- The binary path is not included in `PATH`

**Solution:**
- Verify the installation steps in the documentation
- Ensure the directory containing the urunc binary is added to `PATH`

---

### Unikernel does not boot

**Problem:** The unikernel image is created, but the container fails to start.

**Possible causes:**
- Hypervisor is not installed or not supported
- Required kernel modules are missing
- Image is incompatible with the selected hypervisor

**Solution:**
- Verify hypervisor installation and version
- Check the hypervisor support matrix
- Try running with increased log verbosity

---

### Permission errors

**Problem:** Errors related to permissions or access denied.

**Possible causes:**
- Insufficient user permissions
- Required capabilities not available

**Solution:**
- Run urunc with appropriate permissions
- Review system security settings and documentation

---

### Configuration-related failures

**Problem:** urunc fails due to configuration errors.

**Possible causes:**
- Invalid configuration file
- Deprecated or unsupported configuration options

**Solution:**
- Review the configuration documentation
- Ensure configuration fields match the current urunc version


---

If you encounter an issue not covered here, consider checking existing GitHub
issues or opening a new one with logs and configuration details to help
maintainers investigate.
