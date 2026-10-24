# Building and Running Example Unikernels with urunc

This document explains how to build and run example unikernels using urunc.
It is intended for users who want to quickly experiment with urunc using
real-world applications.

---

## Prerequisites

Before building any unikernel, ensure the following tools are installed:

- Git
- Build tools (make, gcc or clang)
- Docker (optional, but recommended for reproducible builds)
- A working urunc installation

---

## Redis Unikernel Example

Redis is a popular in-memory data store and a good candidate for a unikernel.

### Build Steps

1. Clone the Redis unikernel repository:
   ```
   git clone <redis-unikernel-repository>
   cd redis-unikernel
   ```

2. Build the unikernel image:
   ```
   make
   ```

3. Verify that the image was created successfully.

### Running with urunc

```
urunc run redis.img
```

---

## Nginx Unikernel Example

Nginx can be used to demonstrate HTTP workloads running as unikernels.

### Build Steps

1. Clone the Nginx unikernel repository:
   ```
   git clone <nginx-unikernel-repository>
   cd nginx-unikernel
   ```

2. Build the unikernel:s
   ```
   make
   ```

### Running with urunc

```
urunc run nginx.img
```

---

## Httpreply Unikernel Example

Httpreply is a minimal HTTP service useful for testing networking behavior.

### Build Steps

1. Clone the Httpreply unikernel repository:
   ```
   git clone <httpreply-unikernel-repository>
   cd httpreply-unikernel
   ```

2. Build the unikernel:
   ```
   make
   ```

### Running with urunc

```
urunc run httpreply.img
```

---

## Notes

- Exact build steps may vary depending on the unikernel framework used.
- Docker-based builds are recommended to ensure consistent and reproducible environments.
- These examples can be extended to additional applications and frameworks supported by urunc.
