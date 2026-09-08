import { createResource, createSignal } from '@krate/runtime';

export default function ResourceNestedPage() {
  const [id, setId] = createSignal(1);
  const [user, { refetch }] = createResource(
    () => id(),
    async (n) => `user-${n}`,
  );

  return (
    <div class="page">
      <h1>Nested Resource</h1>
      <p>{user.loading ? 'Loading...' : user()}</p>
      <p>State: {user.state}</p>
      <button onClick={() => refetch()}>Refetch</button>
      <button onClick={() => setId(id() + 1)}>Next</button>
    </div>
  )
}