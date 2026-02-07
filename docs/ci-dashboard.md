---
layout: default
title: "CI Dashboard"
description: "Real-time monitoring of urunc CI workflows"
---

# CI Dashboard

<div id="dashboard-root" class="not-prose">
    <div class="flex items-center justify-center py-20" id="loader">
        <div class="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-500"></div>
    </div>
</div>

<script src="https://cdn.tailwindcss.com"></script>
<script src="https://unpkg.com/lucide@latest"></script>

<script>
    const REPO = 'urunc-dev/urunc';
    const API_BASE = 'https://api.github.com';
    
    const workflows = [
        { id: 'ci_nightly.yml', name: 'Nightly', icon: 'moon' },
        { id: 'ci_on_push.yml', name: 'Pull Requests', icon: 'zap' },
        { id: 'ci_main.yml', name: 'Main Branch', icon: 'sun' }
    ];

    let currentWorkflow = workflows[0].id;
    let runs = [];

    async function fetchData(workflowId) {
        try {
            const response = await fetch(`${API_BASE}/repos/${REPO}/actions/workflows/${workflowId}/runs?per_page=15`);
            const data = await response.json();
            return data.workflow_runs || [];
        } catch (error) {
            console.error('Error fetching runs:', error);
            return [];
        }
    }

    async function fetchJobs(runId) {
        try {
            const response = await fetch(`${API_BASE}/repos/${REPO}/actions/runs/${runId}/jobs`);
            const data = await response.json();
            return data.jobs || [];
        } catch (error) {
            console.error('Error fetching jobs:', error);
            return [];
        }
    }

    function render() {
        const root = document.getElementById('dashboard-root');
        
        const successCount = runs.filter(r => r.conclusion === 'success').length;
        const successRate = runs.length > 0 ? ((successCount / runs.length) * 100).toFixed(1) : 0;

        root.innerHTML = `
            <div class="space-y-8 font-sans text-slate-200">
                <!-- Selector -->
                <div class="flex flex-wrap gap-2 p-1 bg-slate-800/50 rounded-xl w-fit border border-slate-700">
                    ${workflows.map(wf => `
                        <button 
                            onclick="switchWorkflow('${wf.id}')"
                            class="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all ${currentWorkflow === wf.id ? 'bg-blue-600 text-white shadow-lg shadow-blue-900/20' : 'text-slate-400 hover:text-white hover:bg-slate-700'}"
                        >
                            <i data-lucide="${wf.icon}" class="w-4 h-4"></i>
                            ${wf.name}
                        </button>
                    `).join('')}
                </div>

                <!-- Stats -->
                <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                    <div class="bg-slate-800/50 p-6 rounded-2xl border border-slate-700/50 flex items-center justify-between">
                        <div>
                            <p class="text-xs uppercase tracking-wider text-slate-500 font-bold mb-1">Success Rate</p>
                            <h3 class="text-3xl font-black text-white">${successRate}%</h3>
                        </div>
                        <div class="p-3 bg-emerald-500/10 rounded-xl">
                            <i data-lucide="activity" class="text-emerald-400 w-6 h-6"></i>
                        </div>
                    </div>
                    <div class="bg-slate-800/50 p-6 rounded-2xl border border-slate-700/50 flex items-center justify-between">
                        <div>
                            <p class="text-xs uppercase tracking-wider text-slate-500 font-bold mb-1">Total Runs</p>
                            <h3 class="text-3xl font-black text-white">${runs.length}</h3>
                        </div>
                        <div class="p-3 bg-purple-500/10 rounded-xl">
                            <i data-lucide="git-merge" class="text-purple-400 w-6 h-6"></i>
                        </div>
                    </div>
                </div>

                <!-- List -->
                <div class="space-y-3">
                    <h2 class="text-xl font-bold flex items-center gap-2">
                        <i data-lucide="list-tree" class="w-5 h-5 text-blue-500"></i>
                        Recent Activity
                    </h2>
                    <div class="grid gap-3" id="runs-container">
                        ${runs.map(run => renderRunCard(run)).join('')}
                    </div>
                </div>
            </div>
        `;
        
        lucide.createIcons();
    }

    function renderRunCard(run) {
        const isSuccess = run.conclusion === 'success';
        const isFailure = run.conclusion === 'failure';
        const colorClass = isSuccess ? 'border-emerald-500/20 bg-emerald-500/5' : isFailure ? 'border-red-500/20 bg-red-500/5' : 'border-blue-500/20 bg-blue-500/5';
        const accentColor = isSuccess ? 'text-emerald-400' : isFailure ? 'text-red-400' : 'text-blue-400';
        const icon = isSuccess ? 'check-circle' : isFailure ? 'x-circle' : 'loader';

        return `
            <div class="group relative overflow-hidden rounded-xl border p-4 transition-all hover:bg-slate-800/80 ${colorClass}">
                <div class="flex items-center justify-between gap-4">
                    <div class="flex items-center gap-4">
                        <div class="p-2 rounded-lg bg-slate-900/50 ${accentColor}">
                            <i data-lucide="${icon}" class="w-5 h-5 ${!run.conclusion ? 'animate-spin' : ''}"></i>
                        </div>
                        <div>
                            <div class="flex items-center gap-2">
                                <h4 class="font-bold text-slate-100">${run.display_title || run.name}</h4>
                                <span class="text-[10px] px-1.5 py-0.5 rounded-full border border-slate-700 bg-slate-900 font-mono text-slate-500">#${run.run_number}</span>
                            </div>
                            <p class="text-xs text-slate-500 mt-0.5">${new Date(run.created_at).toLocaleString()}</p>
                        </div>
                    </div>
                    <div class="flex items-center gap-2">
                        <button onclick="toggleJobs(${run.id})" class="p-2 rounded-lg bg-slate-900 border border-slate-700 text-slate-400 hover:text-white transition-colors">
                            <i data-lucide="layers" class="w-4 h-4"></i>
                        </button>
                        <a href="${run.html_url}" target="_blank" class="p-2 rounded-lg bg-slate-900 border border-slate-700 text-slate-400 hover:text-white transition-colors">
                            <i data-lucide="external-link" class="w-4 h-4"></i>
                        </a>
                    </div>
                </div>
                <div id="jobs-${run.id}" class="hidden mt-4 pt-4 border-t border-slate-700/50">
                    <div class="grid grid-cols-1 sm:grid-cols-2 gap-2" id="jobs-container-${run.id}">
                        <div class="animate-pulse text-xs text-slate-500">Loading jobs...</div>
                    </div>
                </div>
            </div>
        `;
    }

    async function toggleJobs(runId) {
        const container = document.getElementById(`jobs-${runId}`);
        const content = document.getElementById(`jobs-container-${runId}`);
        
        if (container.classList.contains('hidden')) {
            container.classList.remove('hidden');
            const jobs = await fetchJobs(runId);
            content.innerHTML = jobs.map(job => `
                <div class="flex items-center justify-between p-2 rounded-lg bg-slate-900/50 border border-slate-800">
                    <div class="flex items-center gap-2 truncate">
                        <i data-lucide="${job.conclusion === 'success' ? 'check' : job.conclusion === 'failure' ? 'x' : 'clock'}" 
                           class="w-3 h-3 ${job.conclusion === 'success' ? 'text-emerald-500' : job.conclusion === 'failure' ? 'text-red-500' : 'text-blue-500'}"></i>
                        <span class="text-[11px] font-medium truncate">${job.name}</span>
                    </div>
                    <a href="${job.html_url}" target="_blank" class="text-slate-600 hover:text-slate-400">
                        <i data-lucide="external-link" class="w-3 h-3"></i>
                    </a>
                </div>
            `).join('');
            lucide.createIcons({ scope: container });
        } else {
            container.classList.add('hidden');
        }
    }

    async function switchWorkflow(id) {
        currentWorkflow = id;
        document.getElementById('dashboard-root').innerHTML = `
            <div class="flex items-center justify-center py-20">
                <div class="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-500"></div>
            </div>
        `;
        runs = await fetchData(id);
        render();
    }

    // Initial Load
    (async () => {
        runs = await fetchData(currentWorkflow);
        render();
    })();
</script>

<style>
    /* Prevent MkDocs from messing with our layout */
    .not-prose {
        all: initial;
        display: block;
        color: inherit;
        font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Noto Sans", sans-serif;
    }
    .not-prose * { box-sizing: border-box; }
</style>
