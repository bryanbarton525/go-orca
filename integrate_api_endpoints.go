# File: web/dashboard/dashboard.js

document.addEventListener('DOMContentLoaded', () => {
    const fetchWorkflowsButton = document.getElementById('fetchWorkflowsButton');
    const workflowsList = document.getElementById('workflowsList');
    const tenantIdInput = document.getElementById('tenantIdInput');
    const scopeIdInput = document.getElementById('scopeIdInput');

    fetchWorkflowsButton.addEventListener('click', async () => {
        const tenantId = tenantIdInput.value;
        const scopeId = scopeIdInput.value;
        const response = await fetch('/workflows', {
            method: 'GET',
            headers: {
                'X-Tenant-ID': tenantId,
                'X-Scope-ID': scopeId
            }
        });

        if (!response.ok) {
            workflowsList.innerHTML = `<li>Error fetching workflows: ${response.statusText}</li>`;
            return;
        }

        const workflows = await response.json();
        workflowsList.innerHTML = '';
        workflows.forEach(workflow => {
            const listItem = document.createElement('li');
            listItem.textContent = workflow.name;
            workflowsList.appendChild(listItem);
        });
    });
});