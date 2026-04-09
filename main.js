document.addEventListener('DOMContentLoaded', () => {
    const workflowTableBody = document.getElementById('workflow-list-body');
    const statusFilter = document.getElementById('status-filter');
    const submitForm = document.getElementById('workflow-form');

    /**
     * Fetches workflow data from GET /workflows and renders the table.
     * @param {string} filter - The status filter ('all', 'pending', 'running', 'paused').
     */
    async function fetchAndRenderWorkflows(filter = 'all') {
        workflowTableBody.innerHTML = '<tr><td colspan="4">Loading workflows...</td></tr>';
        try {
            const response = await fetch('/workflows');
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const workflows = await response.json();
            renderWorkflows(workflows, filter);
        } catch (error) {
            console.error("Failed to fetch workflows:", error);
            workflowTableBody.innerHTML = `<tr><td colspan="4" class="text-danger">Error loading workflows: ${error.message}. Check the network console.</td></tr>`;
        }
    }

    /**
     * Renders the provided workflow array into the table body.
     * @param {Array<Object>} workflows - Array of workflow objects.
     * @param {string} currentFilter - The filter that was applied.
     */
    function renderWorkflows(workflows, currentFilter) {
        workflowTableBody.innerHTML = '';
        if (!workflows || workflows.length === 0) {
            workflowTableBody.innerHTML = '<tr><td colspan="4">No workflows found matching the filter.</td></tr>';
            return;
        }

        let filteredWorkflows = workflows;
        if (currentFilter !== 'all') {
            filteredWorkflows = workflows.filter(wf => wf.status === currentFilter);
        }

        if (filteredWorkflows.length === 0) {
            workflowTableBody.innerHTML = `<tr><td colspan="4">No workflows found with status: ${currentFilter}.</td></tr>`;
            return;
        }

        filteredWorkflows.forEach(wf => {
            const row = document.createElement('tr');
            row.innerHTML = `
                <td>${wf.id}</td>
                <td>${wf.name}</td>
                <td>${wf.status}</td>
                <td><button class="btn btn-sm btn-info" onclick="console.log('Viewing details for ${wf.id}')">Details</button></td>
            `;
            workflowTableBody.appendChild(row);
        });
    }

    // --- Event Listeners & Initialization ---

    // 1. Filter Change Listener
    if (statusFilter) {
        statusFilter.addEventListener('change', (event) => {
            const selectedFilter = event.target.value;
            fetchAndRenderWorkflows(selectedFilter);
        });
    }

    // 2. Form Submission Handler
    if (submitForm) {
        submitForm.addEventListener('submit', async (event) => {
            event.preventDefault();
            const formData = new FormData(submitForm);
            const payload = {
                name: formData.get('workflow-name'),
                trigger_args: JSON.parse(formData.get('trigger-args')) || []
            };

            try {
                const response = await fetch('/workflows', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });

                if (!response.ok) {
                    const errorBody = await response.json();
                    throw new Error(errorBody.message || `Failed to submit workflow: ${response.status}`);
                }

                alert('Workflow submitted successfully! Refreshing list...');
                // Re-fetch and re-render to show the new workflow
                await fetchAndRenderWorkflows(statusFilter.value);
                submitForm.reset();
            } catch (error) {
                console.error("Submission Error:", error);
                alert(`Submission Failed: ${error.message}`);
            }
        });
    }

    // Initial load: Fetch all workflows on page load
    fetchAndRenderWorkflows('all');
});