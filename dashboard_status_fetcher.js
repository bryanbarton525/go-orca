/**
 * @fileoverview Client-side logic to fetch and render in-flight workflow statuses.
 * This script assumes the existence of global DOM elements: #workflow-status-container and #status-message.
 */

const STATUS_ENDPOINT = "/api/v1/workflows/status";

/**
 * Generates the appropriate CSS class name based on the workflow status.
 * @param {string} status - The status of the workflow (e.g., 'running', 'pending', 'paused').
 * @returns {string} A class name for styling.
 */
function getStatusClass(status) {
    const normalizedStatus = status.toLowerCase().trim();
    switch (normalizedStatus) {
        case 'running':
            return 'status-running';
        case 'pending':
            return 'status-pending';
        case 'paused':
            return 'status-paused';
        default:
            return 'status-unknown';
    }
}

/**
 * Generates the HTML table rows from the fetched workflow data.
 * @param {Array<{id: string, status: string}>} workflows - Array of workflow objects.
 * @returns {string} The complete HTML string for the table body.
 */
function renderWorkflowTable(workflows) {
    if (!workflows || workflows.length === 0) {
        return "<tr><td colspan='3' class='empty-row'>No workflows are currently in-flight.</td></tr>";
    }

    return workflows.map(workflow => {
        const statusClass = getStatusClass(workflow.status);
        return `
        <tr>
            <td>${workflow.id}</td>
            <td class='status-cell'><span class='status-badge ${statusClass}'>${workflow.status.toUpperCase()}</span></td>
            <td><button class='btn-view-details' data-id='${workflow.id}'>View Details</button></td>
        </tr>`;
    }).join('');
}

/**
 * Fetches the list of active workflows from the status API endpoint and updates the UI.
 * Handles loading, success, and various error states.
 */
async function fetchWorkflowStatus() {
    const statusContainer = document.getElementById('workflow-status-table-body');
    const messageElement = document.getElementById('status-message');

    if (!statusContainer) {
        console.error("Could not find the workflow status table body element.");
        messageElement.textContent = "Error: Dashboard structure corrupted.";
        return;
    }

    // 1. Set Loading State
    statusContainer.innerHTML = ''; // Clear previous results
    messageElement.className = 'message loading';
    messageElement.textContent = "Fetching workflow status... Please wait.";
    // Optionally disable the submit button here

    try {
        // 2. Fetch Data
        const response = await fetch(STATUS_ENDPOINT);

        // 3. Handle HTTP Errors (e.g., 503 Service Unavailable)
        if (!response.ok) {
            let errorDetail = `HTTP error! Status: ${response.status} ${response.statusText}.`;
            // Attempt to read body for more details, helpful for API errors
            try {
                const errorJson = await response.json();
                if (errorJson.error) {
                    errorDetail = `API Error: ${errorJson.error}.`;
                }
            } catch (e) {
                // Ignore if JSON body reading fails
            }
            throw new Error(errorDetail);
        }

        // 4. Parse and Render Data
        const workflows = await response.json();
        
        if (!Array.isArray(workflows)) {
            throw new Error("Received data was not a valid array of workflows.");
        }

        // Success State
        statusContainer.innerHTML = renderWorkflowTable(workflows);
        messageElement.className = 'message success';
        messageElement.textContent = `Successfully loaded ${workflows.length} active workflow(s).`;

    } catch (error) {
        // Error State (Network issues, parsing errors, or thrown API errors)
        console.error("Failed to fetch workflow status:", error);
        statusContainer.innerHTML = ''; // Clear table on error
        messageElement.className = 'message error';
        messageElement.textContent = `Failed to load status: ${error.message}. Please try again later.`;
    } finally {
        // Re-enable components, reset loading state indicator if needed
        // This structure keeps the error message visible until user interaction or page refresh
    }
}

// --- Event Listener Setup ---

// Attach the fetch function to run on initial page load
window.addEventListener('DOMContentLoaded', () => {
    fetchWorkflowStatus();
    // For production use, this should be wrapped in a retry mechanism (e.g., polling every 30s)
});
