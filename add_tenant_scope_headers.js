const tenant = 'defaultTenant';
const scope = 'defaultScope';

async function fetchWorkflows() {
    const response = await fetch('/workflows', {
        headers: {
            'tenant': tenant,
            'scope': scope
        }
    });
    return response.json();
}

async function submitWorkflow(workflowData) {
    const response = await fetch('/workflows', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            'tenant': tenant,
            'scope': scope
        },
        body: JSON.stringify(workflowData)
    });
    return response.json();
}

// Example usage:
fetchWorkflows().then(workflows => console.log(workflows));
submitWorkflow({ name: 'New Workflow', status: 'pending' }).then(response => console.log(response));