#!/usr/bin/env python3
import os
import sys
import json
import requests
import argparse

def get_latest_workflow_run(repo, workflow_id, token):
    """Fetches the latest run for a given workflow."""
    headers = {
        "Accept": "application/vnd.github.v3+json",
        "Authorization": f"token {token}"
    }
    url = f"https://api.github.com/repos/{repo}/actions/workflows/{workflow_id}/runs?per_page=1"
    try:
        response = requests.get(url, headers=headers)
        response.raise_for_status()
        data = response.json()
        if data["workflow_runs"]:
            return data["workflow_runs"][0]
        return None
    except Exception as e:
        print(f"Error fetching workflow runs: {e}")
        return None

def get_run_jobs(repo, run_id, token):
    """Fetches jobs for a given workflow run."""
    headers = {
        "Accept": "application/vnd.github.v3+json",
        "Authorization": f"token {token}"
    }
    url = f"https://api.github.com/repos/{repo}/actions/runs/{run_id}/jobs"
    try:
        response = requests.get(url, headers=headers)
        response.raise_for_status()
        return response.json().get("jobs", [])
    except Exception as e:
        print(f"Error fetching jobs: {e}")
        return []

def send_notification(webhook_url, run_data, repo, failed_jobs=None):
    """Sends a notification to the webhook."""
    status = run_data["conclusion"]
    workflow_name = run_data["name"]
    html_url = run_data["html_url"]
    
    title = f"Workflow {status}: {workflow_name}"
    
    message = f"*{title}*\nRepository: {repo}\n"
    if failed_jobs:
        job_names = ", ".join([job["name"] for job in failed_jobs])
        message += f"Failed Jobs: *{job_names}*\n"
    message += f"<{html_url}|View Logs>"
    
    payload = {
        "text": message
    }
    
    try:
        response = requests.post(webhook_url, json=payload)
        response.raise_for_status()
        print(f"Notification sent for {workflow_name} ({status})")
    except Exception as e:
        print(f"Error sending notification: {e}")

def main():
    parser = argparse.ArgumentParser(description="Check CI status and notify on failure.")
    parser.add_argument("--repo", required=True, help="GitHub repository (owner/name)")
    parser.add_argument("--workflow", required=True, help="Workflow filename or ID")
    parser.add_argument("--token", required=True, help="GitHub Token")
    parser.add_argument("--webhook", required=True, help="Webhook URL for notifications")
    
    args = parser.parse_args()

    print(f"Checking status for {args.workflow} in {args.repo}...")
    run = get_latest_workflow_run(args.repo, args.workflow, args.token)
    
    if run:
        print(f"Latest run ID: {run['id']}, Conclusion: {run['conclusion']}")
        if run['conclusion'] == 'failure':
            print("Run failed. Fetching job details...")
            jobs = get_run_jobs(args.repo, run['id'], args.token)
            failed_jobs = [job for job in jobs if job['conclusion'] == 'failure']
            
            print(f"Found {len(failed_jobs)} failed jobs. Sending notification...")
            send_notification(args.webhook, run, args.repo, failed_jobs)
        else:
            print("Run did not fail. No notification sent.")
    else:
        print("No runs found.")

if __name__ == "__main__":
    main()
