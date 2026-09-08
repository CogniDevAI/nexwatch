import { useState, useEffect, useCallback } from "react";
import { Navigate } from "react-router-dom";
import { Plus, Trash2, UserPlus, Users as UsersIcon } from "lucide-react";
import pb from "@/lib/pocketbase";
import { useAuthStore } from "@/stores/authStore";
import type { User, Role } from "@/types";
import { PageHeader } from "@/components/ui/PageHeader";
import { Panel } from "@/components/ui/Panel";
import { Table, Th, Td } from "@/components/ui/Table";
import { rowClass } from "@/components/ui/rowClass";
import { Modal } from "@/components/ui/Modal";
import { Input, Select, Label } from "@/components/ui/Field";
import { Button, IconButton } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { useToast } from "@/components/ui/toastContext";
import { usePageTitle } from "@/hooks/usePageTitle";

const ROLE_OPTIONS: Role[] = ["viewer", "operator", "admin"];

function formatDate(dateStr: string): string {
  if (!dateStr) return "—";
  try {
    return new Date(dateStr).toLocaleString();
  } catch {
    return dateStr;
  }
}

export function Users() {
  usePageTitle("Users");

  const hasRole = useAuthStore((s) => s.hasRole);
  const currentUserId = useAuthStore((s) => s.user?.id);
  const { showToast } = useToast();

  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);
  const [updatingRoleId, setUpdatingRoleId] = useState<string | null>(null);

  const fetchUsers = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const records = await pb.collection("users").getFullList<User>({
        sort: "-created",
      });
      setUsers(records);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load users");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchUsers();
  }, [fetchUsers]);

  // Only admins may reach this page — anyone else is redirected back to
  // the general Settings page.
  if (!hasRole("admin")) {
    return <Navigate to="/settings" replace />;
  }

  const handleRoleChange = async (userId: string, role: Role) => {
    setUpdatingRoleId(userId);
    try {
      await pb.collection("users").update(userId, { role });
      setUsers((prev) => prev.map((u) => (u.id === userId ? { ...u, role } : u)));
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to update role", "error");
    } finally {
      setUpdatingRoleId(null);
    }
  };

  const handleDelete = async (userId: string) => {
    try {
      await pb.collection("users").delete(userId);
      setUsers((prev) => prev.filter((u) => u.id !== userId));
      setDeleteConfirm(null);
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Failed to delete user", "error");
    }
  };

  const handleFormSave = () => {
    setShowForm(false);
    void fetchUsers();
  };

  return (
    <div>
      <PageHeader
        title="Users"
        actions={
          <Button variant="primary" onClick={() => setShowForm(true)}>
            <Plus className="h-4 w-4" aria-hidden="true" />
            Add user
          </Button>
        }
      />

      <Panel>
        {loading ? (
          <div className="p-5">
            <Skeleton className="h-40 w-full" />
          </div>
        ) : error ? (
          <ErrorState
            title="Couldn't load users"
            description={error}
            action={
              <Button variant="primary" size="sm" onClick={() => void fetchUsers()}>
                Try again
              </Button>
            }
          />
        ) : users.length === 0 ? (
          <EmptyState
            icon={UsersIcon}
            title="No users yet"
            description="Add the first dashboard account to get started."
          />
        ) : (
          <Table>
            <thead>
              <tr className="border-b border-[var(--color-line)]">
                <Th>Email</Th>
                <Th>Name</Th>
                <Th>Role</Th>
                <Th>Created</Th>
                <Th align="right">Actions</Th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[var(--color-line-soft)]">
              {users.map((u, idx) => (
                <tr key={u.id} className={rowClass(idx)}>
                  <Td className="font-medium">{u.email}</Td>
                  <Td className="text-[var(--color-ink-muted)]">{u.name || "—"}</Td>
                  <Td>
                    <Select
                      value={u.role}
                      disabled={updatingRoleId === u.id || u.id === currentUserId}
                      onChange={(e) => handleRoleChange(u.id, e.target.value as Role)}
                      title={u.id === currentUserId ? "You cannot change your own role" : undefined}
                      aria-label={`Role for ${u.email}`}
                      className="w-auto"
                      selectClassName="!py-1 !text-xs"
                    >
                      {ROLE_OPTIONS.map((role) => (
                        <option key={role} value={role}>
                          {role}
                        </option>
                      ))}
                    </Select>
                  </Td>
                  <Td className="text-xs whitespace-nowrap text-[var(--color-ink-faint)]">
                    {formatDate(u.created)}
                  </Td>
                  <Td align="right">
                    {deleteConfirm === u.id ? (
                      <div className="flex items-center justify-end gap-1">
                        <Button size="sm" variant="danger" onClick={() => handleDelete(u.id)}>
                          Confirm
                        </Button>
                        <Button size="sm" variant="ghost" onClick={() => setDeleteConfirm(null)}>
                          Cancel
                        </Button>
                      </div>
                    ) : (
                      <IconButton
                        aria-label={
                          u.id === currentUserId
                            ? "You cannot delete your own account"
                            : `Delete user ${u.email}`
                        }
                        onClick={() => setDeleteConfirm(u.id)}
                        disabled={u.id === currentUserId}
                        title={
                          u.id === currentUserId
                            ? "You cannot delete your own account"
                            : "Delete user"
                        }
                        className="hover:!bg-[var(--color-critical)]/10 hover:!text-[var(--color-critical)]"
                      >
                        <Trash2 className="h-4 w-4" />
                      </IconButton>
                    )}
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>

      {showForm && <UserForm onSave={handleFormSave} onClose={() => setShowForm(false)} />}
    </div>
  );
}

// --- Create User Form ---

interface UserFormProps {
  onSave: () => void;
  onClose: () => void;
}

function UserForm({ onSave, onClose }: UserFormProps) {
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (password !== passwordConfirm) {
      setError("Passwords do not match.");
      return;
    }

    setSaving(true);
    try {
      await pb.collection("users").create({
        email,
        password,
        passwordConfirm,
        name,
        role,
        emailVisibility: true,
      });
      onSave();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create user");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title={
        <span className="flex items-center gap-2">
          <UserPlus className="h-4 w-4" aria-hidden="true" />
          Add user
        </span>
      }
      onClose={onClose}
      maxWidth="max-w-md"
    >
      <form onSubmit={handleSubmit} className="space-y-4 p-6">
        {error && (
          <div
            role="alert"
            className="rounded-[var(--radius-control)] border border-[var(--color-critical)]/25 bg-[var(--color-critical)]/10 px-4 py-2 text-sm text-[var(--color-critical)]"
          >
            {error}
          </div>
        )}

        <div>
          <Label htmlFor="new-user-email">Email</Label>
          <Input
            id="new-user-email"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            placeholder="user@example.com"
          />
        </div>

        <div>
          <Label htmlFor="new-user-name">Name</Label>
          <Input
            id="new-user-name"
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Optional"
          />
        </div>

        <div>
          <Label htmlFor="new-user-role">Role</Label>
          <Select id="new-user-role" value={role} onChange={(e) => setRole(e.target.value as Role)}>
            {ROLE_OPTIONS.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </Select>
        </div>

        <div>
          <Label htmlFor="new-user-password">Password</Label>
          <Input
            id="new-user-password"
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            minLength={8}
            placeholder="At least 8 characters"
          />
        </div>

        <div>
          <Label htmlFor="new-user-password-confirm">Confirm password</Label>
          <Input
            id="new-user-password-confirm"
            type="password"
            value={passwordConfirm}
            onChange={(e) => setPasswordConfirm(e.target.value)}
            required
            minLength={8}
          />
        </div>

        <div className="flex items-center justify-end gap-3 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={saving}>
            {saving ? "Creating…" : "Create user"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
