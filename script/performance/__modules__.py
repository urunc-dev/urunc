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

from json import loads, dump
from typing import List, Tuple, Dict
from subprocess import run, PIPE


class Timestamp:
    def __init__(self, tsID: str, timestamp: int) -> None:
        self.tsID = tsID
        self.timestamp = timestamp

    def __repr__(self) -> str:
        return f'{self.tsID}: {self.timestamp}'

    def __sub__(self, other):
        return int(self.timestamp - other.timestamp)

    @classmethod
    def fromLogLine(cls, logLine: str):
        temp = loads(logLine)
        tsID = temp["timestampID"]
        timestamp = int(temp["time"])
        return cls(tsID=tsID, timestamp=timestamp)


class TimestampDiff:
    startTsID: str
    stopTsID: str
    tdID: str
    duration: int

    def __init__(self, startTsID: str, stopTsID: str, duration: int) -> None:
        self.startTsID = startTsID
        self.stopTsID = stopTsID
        self.duration = duration
        self.tdID = f"{startTsID} -> {stopTsID}"

    def __str__(self) -> str:
        return f"{self.startTsID} -> {self.stopTsID}:\t{self.duration} ns"

    @classmethod
    def fromTimestamps(cls, start: Timestamp, stop: Timestamp):
        duration = stop-start
        return cls(startTsID=start.tsID, stopTsID=stop.tsID, duration=duration)


class TimestampSeries:

    timestamps: List[Timestamp]
    containerID: str

    def __init__(self, data: List[str]) -> None:
        self.timestamps = []
        for line in data:
            self.timestamps.append(Timestamp.fromLogLine(logLine=line))
        self.containerID = loads(data[0])['containerID']

    def __str__(self) -> str:
        msg = ""
        for ts in self.sorted:
            msg += f'{ts.tsID}:  {ts.timestamp}\n'
        msg += "\n"
        for i in range(len(self.common)):
            cts = self.common[i]
            msg += f'{cts[0].tsID}: {cts[0].timestamp}\n'
            msg += f'{cts[1].tsID}: {cts[1].timestamp}\n\n'
        msg = msg[:-2]
        return msg

    @property
    def report(self) -> str:
        msg = ""
        for i in range(len(self.sorted)-1):
            diff = TimestampDiff.fromTimestamps(
                start=self.sorted[i], stop=self.sorted[i+1])
            msg += f"{diff}\n"
        msg = msg[:-1]
        return msg

    @property
    def diffs(self) -> Dict[str, TimestampDiff]:
        diffs = {}
        for i in range(len(self.sorted)-1):
            diff = TimestampDiff.fromTimestamps(
                start=self.sorted[i], stop=self.sorted[i+1])
            diffs[diff.tdID] = diff
        return diffs

    @property
    def common(self) -> List[Tuple[Timestamp]]:
        temp = []
        for i in range(len(self.timestamps)):
            ts = self.timestamps[i]
            if ts.tsID == 'cTS00':
                this = (ts, self.timestamps[i+1])
                temp.append(this)
        return temp

    @property
    def sorted(self) -> List[Timestamp]:
        unique = [ts for ts in self.timestamps if 'cTS' not in ts.tsID]
        temp = [ts for ts in unique]
        temp.sort(key=lambda x: int(x.timestamp))
        return temp


def parseSingleContainerTimestamps(
    filename: str, containerID: str
) -> List[str]:
    with open(filename, 'r') as f:
        data = f.readlines()
        return [line for line in data if containerID in line]


def emptyFile(filename: str) -> None:
    open(filename, "w").close()


def spawnContainer(
    image: str = (
        "harbor.nbfc.io/nubificus/urunc/redis-hvt-rumprun:latest"
    ),
    name: str = "redis-test",
    snapshotter: str = "devmapper",
    runtime: str = "io.containerd.uruncts.v2"
) -> str:
    command = (
        f"nerdctl run --name {name} -d"
        f" --snapshotter {snapshotter}"
        f" --runtime {runtime} {image}"
    )
    cmdParts = command.split(" ")
    cmd = run(cmdParts,
              stdout=PIPE,
              text=True)
    response = cmd.stdout
    containerID = response.splitlines()[-1]
    return containerID


def deleteContainer(name: str = "redis-test") -> bool:
    command = f"nerdctl rm --force {name}"
    cmdParts = command.split(" ")
    cmd = run(cmdParts,
              stdout=PIPE,
              text=True)
    response = cmd.stdout
    result = response.splitlines()[-1]
    return result == name


def myprint(msg: str):
    print(msg)
    clear_line()


def clear_line(n=1):
    line_up = '\033[1A'
    line_clear = '\x1b[2K'
    for i in range(n):
        print(line_up, end=line_clear)


def saveToJsonFile(filename: str, data: dict):
    with open(filename, "w") as file:
        dump(data, file)
