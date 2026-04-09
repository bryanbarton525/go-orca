# Pull Request Description

This pull request introduces a demo dashboard to the go-orca repository, located at `web/dashboard/index.html` and `web/dashboard/dashboard.js`. The dashboard interacts with existing API server routes/handlers for retrieving and posting workflows. The following changes have been made:

## Changes Made:

1. **HTML Structure (`web/dashboard/index.html`)**
   - Created the HTML structure for the demo dashboard, including input fields for tenant ID and scope ID, a button to fetch workflows, and an unordered list to display the fetched workflows.
   - Corrected the typo in the JavaScript source reference from `src="dashboard.js"` to `src="./dashboard.js"`.

2. **JavaScript Logic (`web/dashboard/dashboard.js`)**
   - Implemented JavaScript logic to interact with API server routes, filter in-flight statuses (pending/running/paused), and handle X-Tenant-ID/X-Scope-ID headers from user inputs.
   - Corrected typos in input element IDs from `tenantIdInput` and `scopeIdInput` to match the HTML element IDs (`tenantId` and `scopeId`).

3. **Route Definitions (`internal/api/routes/routes.go`)**
   - Updated the internal/api/routes/routes.go file to include placeholders for future dashboard routes, ensuring that all defined HTTP methods and endpoints are correctly associated with their respective handler functions.

4. **Handler Logic (`internal/api/handlers/handlers.go`)**
   - Updated internal/api/handlers/handlers.go to include logic for handling dashboard requests, ensuring in-flight status filtering and header management.

5. **Test Updates (`internal/api/handlers/handlers_test.go`)**
   - Updated tests in handlers_test.go to validate the new handler logic for filtering in-flight statuses and handling X-Tenant-ID/X-Scope-ID headers.

6. **Routing Fixes**
   - Fixed routing issues in internal/api/routes/routes.go by ensuring all defined HTTP methods and endpoints are correctly associated with their respective handler functions.

## QA Blocking Issues Addressed:

- Corrected the typo in the JavaScript source reference from `src="dashboard.js"` to `src="./dashboard.js"` in `web/dashboard/index.html`.
- Corrected typos in input element IDs in the JavaScript file for the dashboard.
- Fixed routing issues in internal/api/routes/routes.go by ensuring all defined HTTP methods and endpoints are correctly associated with their respective handler functions.

## Testing:

- Ran `go test ./...` and fixed any identified issues until all tests passed.

This pull request ensures that the demo dashboard is integrated smoothly with the existing API server routes/handlers, providing a user-friendly interface for managing workflows while adhering to project requirements.