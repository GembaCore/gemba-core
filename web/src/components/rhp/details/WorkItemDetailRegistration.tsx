// WorkItemDetailRegistration — registers the 'workitem' detail-tab kind
// with the RHP detail-content registry (gm-root.22.5).
//
// Mount this alongside <HelpTab /> in AppShell so the registration is
// active for the lifetime of the app, not just when a board route mounts.
// Parallel registrations (.6 EpicDetail, .7 RecommendOrderDetail) follow
// the same pattern — each adds a sibling mount here.
//
// This component renders null. All output goes through the RHP shell
// via the registered content renderer.

import { useEffect } from 'react';
import { FileText } from 'lucide-react';
import { useRhpDetailRegistry } from '../RhpContext';
import { WorkItemDetail } from './WorkItemDetail';

export function WorkItemDetailRegistration() {
  const registry = useRhpDetailRegistry();

  useEffect(() => {
    return registry.register({
      kind: 'workitem',
      icon: FileText,
      label: 'Work item',
      render: (id) => <WorkItemDetail id={id} />,
    });
  }, [registry]);

  return null;
}
