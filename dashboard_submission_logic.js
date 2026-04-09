// Handles the submission of a new workflow request from the form.

const submitForm = document.getElementById('workflow-submission-form');
const statusMessage = document.getElementById('submission-status-message');

/**
 * Displays a message to the user in the designated status area.
 * @param {string} message The text to display.
 * @param {string} type 'success', 'error', or 'info' for styling.
 */
function displayStatus(message, type = 'info') {
    statusMessage.textContent = message;
    statusMessage.className = `alert alert-${type}`;
    statusMessage.style.display = 'block';
}

/**
 * Clears all status messages and resets the form state.
 */
function clearStatus() {
    statusMessage.textContent = '';
    statusMessage.className = 'alert';
    statusMessage.style.display = 'none';
}

/**
 * Validates the form inputs before submission.
 * @param {object} data Object containing form values.
 * @returns {boolean} True if validation passes, false otherwise.
 */
function validateSubmission(data) {
    let isValid = true;
    if (!data.templateId || data.templateId.trim() === '') {
        displayStatus('Workflow Template ID is required.', 'error');
        isValid = false;
    }
    // Basic check for required parameters defined in the design
    if (!data.parameters || typeof data.parameters !== 'object' || Object.keys(data.parameters).length === 0) {
        displayStatus('Workflow Parameters must include at least one required key-value pair.', 'error');
        isValid = false;
    }
    return isValid;
}

/**
 * Handles the entire form submission process.
 * Intercepts the native submit event, performs validation, and calls the API.
 * @param {Event} event The submit event.
 */
async function handleWorkflowSubmission(event) {
    event.preventDefault();
    clearStatus();

    const formData = new FormData(event.target);
    const payload = {
        templateId: formData.get('template-id') || '',
        parameters: JSON.parse(formData.get('parameters')) || {}
    };

    if (!validateSubmission(payload)) {
        return;
    }

    displayStatus('Submitting workflow request...', 'info');
    const submitUrl = '/api/v1/workflows/submit';

    try {
        const response = await fetch(submitUrl, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(payload),
        });

        const data = await response.json();

        if (response.ok && data.success) {
            displayStatus(`Success! Workflow requested. ID: ${data.workflowId || 'N/A'}. Check status updates below.`, 'success');
            // Optionally refresh the status panel after successful submission
            fetchWorkflowStatus();
        } else {
            // Handle structured API errors
            const errorMessage = data.error || 'An unknown error occurred while submitting the workflow.';
            displayStatus(`Submission Failed: ${errorMessage}`, 'error');
        }
    } catch (error) {
        // Handle network errors (e.g., server unreachable)
        console.error('Network error during submission:', error);
        displayStatus('Network Error: Could not connect to the API endpoint. Please check the console for details.', 'error');
    }
}

// Attach the event listener when the DOM is fully loaded
document.addEventListener('DOMContentLoaded', () => {
    const form = document.getElementById('workflow-submission-form');
    if (form) {
        form.addEventListener('submit', handleWorkflowSubmission);
    }
});
