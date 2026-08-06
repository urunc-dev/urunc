#!/bin/bash -eu

cd $SRC/urunc

# Compile fuzzers
compile_go_fuzzer github.com/urunc-dev/urunc/pkg/unikontainers FuzzUruncConfigFromMap fuzz_urunc_config_from_map
compile_go_fuzzer github.com/urunc-dev/urunc/pkg/unikontainers FuzzUnikernelConfigDecode fuzz_unikernel_config_decode
compile_go_fuzzer github.com/urunc-dev/urunc/pkg/unikontainers FuzzGetUnikernelConfig fuzz_get_unikernel_config
compile_go_fuzzer github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors FuzzBytesToStringMB fuzz_bytes_to_string_mb
compile_go_fuzzer github.com/urunc-dev/urunc/pkg/unikontainers/unikernels FuzzSubnetMaskToCIDR fuzz_subnet_mask_to_cidr
