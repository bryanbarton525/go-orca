document.addEventListener('DOMContentLoaded', () => {
    const tenantIdInput = document.getElementById('tenantId');
    const scopeIdInput = document.getElementById('scopeId');
    const fetchButton = document.getElementById('fetchWorkflowsButton');
    const workflowList = document.getElementById('workflowList');

    fetchButton.addEventListener('click', async () => {
        const tenantId = tenantIdInput.value;
        const scopeId = scopeIdInput.value;

        try {
            const response = await fetch('/workflows', {
                method: 'GET',
                headers: {
                    'X-Tenant-ID': tenantId,
                    'X-Scope-ID': scopeId
                }
            });

            if (!response.ok) {
                throw new Error('Network response was not ok');
            }

            const workflows = await response.json();
            const inFlightWorkflows = workflows.filter(workflow => ['pending', 'running', 'paused'].includes(workflow.status));

            workflowList.innerHTML = '';
            inFlightWorkflows.forEach(workflow => {
                const listItem = document.createElement('li');
                listItem.textContent = `${workflow.id}: ${workflow.status}`;
                workflowList.appendChild(listItem);
            });
        } catch (error) {
            console.error('Fetch error:', error);
        }
    });
});