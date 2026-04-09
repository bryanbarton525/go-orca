// dashboard.js

document.addEventListener('DOMContentLoaded', () => {
    const fetchWorkflows = async () => {
        try {
            const response = await fetch('/workflows', {
                headers: {
                    'Tenant': localStorage.getItem('tenant'),
                    'Scope': localStorage.getItem('scope')
                }
            });
            if (!response.ok) {
                throw new Error('Failed to fetch workflows');
            }
            const workflows = await response.json();
            displayWorkflows(workflows);
        } catch (error) {
            console.error('Error fetching workflows:', error);
        }
    };

    const displayWorkflows = (workflows) => {
        const pendingList = document.getElementById('pending-list');
        const runningList = document.getElementById('running-list');
        const pausedList = document.getElementById('paused-list');

        workflows.forEach(workflow => {
            const li = document.createElement('li');
            li.textContent = workflow.id + ' - ' + workflow.status;

            switch (workflow.status) {
                case 'pending':
                    pendingList.appendChild(li);
                    break;
                case 'running':
                    runningList.appendChild(li);
                    break;
                case 'paused':
                    pausedList.appendChild(li);
                    break;
                default:
                    console.warn('Unknown workflow status:', workflow.status);
            }
        });
    };

    const submitWorkflow = async (event) => {
        event.preventDefault();
        const form = document.getElementById('workflow-form');
        const data = new FormData(form);
        const body = Object.fromEntries(data.entries());

        try {
            const response = await fetch('/workflows', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Tenant': localStorage.getItem('tenant'),
                    'Scope': localStorage.getItem('scope')
                },
                body: JSON.stringify(body)
            });

            if (!response.ok) {
                throw new Error('Failed to submit workflow');
            }
            alert('Workflow submitted successfully!');
            form.reset();
        } catch (error) {
            console.error('Error submitting workflow:', error);
            alert('Failed to submit workflow. Please try again.');
        }
    };

    document.getElementById('workflow-form').addEventListener('submit', submitWorkflow);

    fetchWorkflows();
});