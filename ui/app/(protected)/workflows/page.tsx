import { Suspense } from "react";
import { WorkflowStudio } from "../../../components/workflow-studio";

export default function WorkflowsPage() {
  return (
    <Suspense>
      <WorkflowStudio />
    </Suspense>
  );
}