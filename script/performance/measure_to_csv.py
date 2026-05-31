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

from __modules__ import (
    emptyFile, spawnContainer, deleteContainer,
    parseSingleContainerTimestamps, TimestampSeries, myprint
)
from sys import argv
from time import sleep
import csv

LOGFILE = "/tmp/urunc.zlog"
DELAY = 2

# Key phase definitions: (start_tsID, end_tsID, column_name)
PHASES = [
    ("TS00", "TS11", "create_ns"),   # full create phase
    ("TS12", "TS19", "start_ns"),    # full start phase
    ("TS16", "TS17", "network_ns"),  # network setup
    ("TS17", "TS18", "disk_ns"),     # disk setup
]


def get_phase_duration(series, start_id, end_id):
    ts_map = {ts.tsID: ts for ts in series.sorted}
    if start_id in ts_map and end_id in ts_map:
        return ts_map[end_id].timestamp - ts_map[start_id].timestamp
    return "N/A"


def main():
    if len(argv) != 4:
        print("Error: Missing arguments!")
        print("")
        print("Usage:")
        print(f"\t{argv[0]} <ITERATIONS> <IMAGE> <OUTPUT_CSV>")
        print("")
        print("Example:")
        print(f"\t{argv[0]} 5 "
              "harbor.nbfc.io/nubificus/urunc/"
              "redis-hvt-rumprun:latest metrics.csv")
        exit(1)

    iterations = int(argv[1])
    image = argv[2]
    output_file = argv[3]
    name = "urunc-metrics-test"

    myprint(f"Collecting metrics for {iterations} iterations")
    myprint(f"Image: {image}")
    sleep(2)

    emptyFile(LOGFILE)
    container_ids = []

    for i in range(iterations):
        myprint(f"Running iteration {i+1} of {iterations}")
        container_id = spawnContainer(image=image, name=name)
        container_ids.append(container_id)
        sleep(DELAY)
        success = deleteContainer(name=name)
        if not success:
            print("Error removing container.")
            exit(1)

    myprint("Writing CSV...")

    with open(output_file, "w", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["containerID", "create_ns", "start_ns",
                         "network_ns", "disk_ns"])
        for container_id in container_ids:
            data = parseSingleContainerTimestamps(
                filename=LOGFILE, containerID=container_id)
            if not data:
                continue
            series = TimestampSeries(data=data)
            row = [container_id]
            for start_id, end_id, _ in PHASES:
                row.append(get_phase_duration(series, start_id, end_id))
            writer.writerow(row)

    myprint(f"Saved metrics to {output_file}")
    emptyFile(LOGFILE)


if __name__ == "__main__":
    main()
