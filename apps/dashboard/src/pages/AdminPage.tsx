import { useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import {
  useCreateProjectKeyMutation,
  useCreateProjectMutation,
} from "../features/reporting/reportingApi";

export function AdminPage() {
  const [name, setName] = useState("");
  const [site, setSite] = useState("");
  const [label, setLabel] = useState("default-key");
  const [projectId, setProjectId] = useState("");
  const [newKeyLabel, setNewKeyLabel] = useState("rotation-key");
  const [resultMessage, setResultMessage] = useState("");

  const [createProject, { isLoading: isCreatingProject }] = useCreateProjectMutation();
  const [createProjectKey, { isLoading: isCreatingKey }] = useCreateProjectKeyMutation();

  const handleCreateProject = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    try {
      const result = await createProject({ name, site, label }).unwrap();
      setProjectId(result.project.id);
      setResultMessage(
        `Created ${result.project.id}. Store this API key now: ${result.apiKey}`,
      );
    } catch {
      setResultMessage("Project creation failed. Check ADMIN_API_KEY and payload values.");
    }
  };

  const handleCreateProjectKey = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    try {
      const result = await createProjectKey({ projectId, label: newKeyLabel }).unwrap();
      setResultMessage(
        `Created key ${result.apiKeyId} for ${result.projectId}. Store this API key now: ${result.apiKey}`,
      );
    } catch {
      setResultMessage("API key creation failed. Verify project ID and admin auth.");
    }
  };

  return (
    <section>
      <div className="session-heading">
        <h1>Admin</h1>
        <Link to="/">Back to issues</Link>
      </div>
      <p>Create projects and issue project-specific API keys for SDK integrations.</p>

      <div className="admin-grid">
        <form className="admin-card" onSubmit={handleCreateProject}>
          <h2>Create Project</h2>
          <label htmlFor="project-name">Project name</label>
          <input
            id="project-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Checkout Monitoring"
            required
          />

          <label htmlFor="project-site">Site</label>
          <input
            id="project-site"
            value={site}
            onChange={(event) => setSite(event.target.value)}
            placeholder="shop.example.com"
            required
          />

          <label htmlFor="project-label">Initial key label</label>
          <input
            id="project-label"
            value={label}
            onChange={(event) => setLabel(event.target.value)}
            placeholder="default-key"
            required
          />

          <button type="submit" disabled={isCreatingProject}>
            {isCreatingProject ? "Creating..." : "Create project + key"}
          </button>
        </form>

        <form className="admin-card" onSubmit={handleCreateProjectKey}>
          <h2>Create Project Key</h2>
          <label htmlFor="project-id">Project ID</label>
          <input
            id="project-id"
            value={projectId}
            onChange={(event) => setProjectId(event.target.value)}
            placeholder="proj_..."
            required
          />

          <label htmlFor="new-key-label">Key label</label>
          <input
            id="new-key-label"
            value={newKeyLabel}
            onChange={(event) => setNewKeyLabel(event.target.value)}
            placeholder="rotation-key"
            required
          />

          <button type="submit" disabled={isCreatingKey}>
            {isCreatingKey ? "Creating..." : "Issue key"}
          </button>
        </form>
      </div>

      {resultMessage && <p className="admin-result">{resultMessage}</p>}
    </section>
  );
}
