// Stands in for a definition go_solid will synthesise from the route table.
// The scenario copies it into <Workspace>/types before booting.
export interface Navigation {
  currentPath: string;
  backHref?: string;
}
