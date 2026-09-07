//go:build linux
// +build linux

// Copyright (c) 2023-2026, Nubificus LTD
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"regexp"
	"time"

	"github.com/sirupsen/logrus"
)

var msgRegex = regexp.MustCompile(`^([\w\-]+\[\d+\]): (.+)$`)

type StructuredJSONFormatter struct{}

func (f *StructuredJSONFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	data := make(logrus.Fields, len(entry.Data)+4)
	maps.Copy(data, entry.Data)

	data["time"] = entry.Time.Format(time.RFC3339)
	data["level"] = entry.Level.String()

	if matches := msgRegex.FindStringSubmatch(entry.Message); len(matches) == 3 {
		data["subsystem"] = matches[1]
		data["msg"] = matches[2]
	} else {
		data["msg"] = entry.Message
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)

	err := encoder.Encode(data)
	return buf.Bytes(), err
}

// ForwardLogs reads logs from the provided logPipe and forwards them to the standard logger.
// It returns a channel that will receive an error if reading from the logPipe fails.
//
// Modified version of runc's ForwardLogs:
// https://github.com/opencontainers/runc/blob/b55b308143715914bd3569727237bb0b0ddb62bd/libcontainer/logs/logs.go#11
func ForwardLogs(logPipe io.ReadCloser) chan error {
	done := make(chan error, 1)
	s := bufio.NewScanner(logPipe)

	logger := logrus.StandardLogger()

	if logger.ReportCaller {
		// Need a copy of the standard logger, but with ReportCaller
		// turned off, as the logs are merely forwarded and their
		// true source is not this file/line/function.
		logNoCaller := *logrus.StandardLogger()
		logNoCaller.ReportCaller = false
		logger = &logNoCaller
	}
	switch logger.Formatter.(type) {
	case *logrus.JSONFormatter:
		logger.SetFormatter(&StructuredJSONFormatter{})
	default:
		logger.SetFormatter(&logrus.TextFormatter{
			DisableColors: true,
			ForceQuote:    true,
		})
	}

	go func() {
		for s.Scan() {
			processEntry(s.Bytes(), logger)
		}
		if err := logPipe.Close(); err != nil {
			logrus.Errorf("error closing log source: %v", err)
		}
		// The only error we want to return is when reading from
		// logPipe has failed.
		done <- s.Err()
		close(done)
	}()

	return done
}

// processEntry processes a log entry, decoding it from JSON and logging it using the provided logger.
// It expects the log entry to be in a specific JSON format
// with "level" and "msg" fields. If the entry cannot be decoded, it logs an error.
//
// Modified version of runc's processEntry:
// https://github.com/opencontainers/runc/blob/b55b308143715914bd3569727237bb0b0ddb62bd/libcontainer/logs/logs.go#41
func processEntry(text []byte, logger *logrus.Logger) {
	if len(text) == 0 {
		return
	}

	var jl struct {
		Level logrus.Level `json:"level"`
		Msg   string       `json:"msg"`
	}
	if err := json.Unmarshal(text, &jl); err != nil {
		logrus.Errorf("failed to decode %q to json: %v", text, err)
		return
	}
	logger.Log(jl.Level, jl.Msg)
}
