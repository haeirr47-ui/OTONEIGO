import { createContext, useContext } from "react";
import type { EntitySearchKind } from "./contracts";

// A detail page is opened from a search result card or a track action menu. The
// App owns the panel so the queue/player remain the only stateful owners.
export interface EntityViewRequest {
  kind: EntitySearchKind;
  id: string;
  name: string;
}

export const EntityViewContext = createContext<((request: EntityViewRequest) => void) | null>(null);
export function useEntityView() {
  return useContext(EntityViewContext);
}
