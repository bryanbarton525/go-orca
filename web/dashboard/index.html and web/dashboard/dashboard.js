**web/dashboard/index.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Go Orca Dashboard</title>
    <script src="/dashboard/dashboard.js"></script>
</head>
<body>
    <h1>Go Orca Workflow Dashboard</h1>
    <div id="workflows"></div>
    <button onclick="fetchWorkflows()">Fetch Workflows</button>
</body>
</html>
```

**web/dashboard/dashboard.js**

```javascript
async function fetchWorkflows() {
    const tenantId = prompt('Enter X-Tenant-ID');
    const scopeId = prompt('Enter X-Scope-ID');

    if (!tenantId || !scopeId) {
        alert('Tenant ID and Scope ID are required.');
        return;
    }

    try {
        const response = await fetch('/workflows', {
            method: 'GET',
            headers: {
                'X-Tenant-ID': tenantId,
                'X-Scope-ID': scopeId
            }
        });

        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }

        const data = await response.json();
        displayWorkflows(data);
    } catch (error) {
        console.error('Failed to fetch workflows:', error);
        alert('Failed to fetch workflows. Please check your inputs and try again.');
    }
}

function displayWorkflows(workflows) {
    const workflowsDiv = document.getElementById('workflows');
    workflowsDiv.innerHTML = ''; // Clear previous content

    if (workflows.length === 0) {
        workflowsDiv.textContent = 'No workflows found.';
        return;
    }

    const ul = document.createElement('ul');
    workflows.forEach(workflow => {
        const li = document.createElement('li');
        li.textContent = `ID: ${workflow.id}, Name: ${workflow.name}`;
        ul.appendChild(li);
    });

    workflowsDiv.appendChild(ul);
}
```
