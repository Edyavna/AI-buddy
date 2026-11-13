import type { Group } from "./api";

export const editFields = ["title", "resolution", "questionIds"] as const;
export type EditField = (typeof editFields)[number];
type Values = Pick<Group, "title" | "resolution"> & { questionIds: string[] };
export type GroupEdit = {
  values: Values;
  base: Values;
  revision: number;
  conflicts: Record<EditField, boolean>;
};

export function groupEdit(group: Group): GroupEdit {
  const values = {
    title: group.title,
    resolution: group.resolution,
    questionIds: group.members.map((m) => m.id).sort(),
  };
  return {
    values,
    base: values,
    revision: group.revision,
    conflicts: { title: false, resolution: false, questionIds: false },
  };
}

function mergeValue<T>(local: T, base: T, remote: T, unresolved: boolean) {
  const same = (a: T, b: T) => JSON.stringify(a) === JSON.stringify(b);
  return {
    value: same(local, base) && !unresolved ? remote : local,
    conflict:
      !same(local, remote) &&
      (unresolved || (!same(local, base) && !same(remote, base))),
  };
}

// Advance the revision together with its baseline. Re-reading an unresolved
// conflict must never turn the local variant into an implicitly approved edit.
export function refreshGroupEdit(
  edit: GroupEdit,
  group: Group,
  archive = false,
): GroupEdit {
  const remote = groupEdit(group);
  const title = mergeValue(
    edit.values.title,
    edit.base.title,
    remote.values.title,
    edit.conflicts.title,
  );
  const resolution = mergeValue(
    edit.values.resolution,
    edit.base.resolution,
    remote.values.resolution,
    edit.conflicts.resolution,
  );
  const members = mergeValue(
    edit.values.questionIds.toSorted(),
    edit.base.questionIds,
    remote.values.questionIds,
    edit.conflicts.questionIds,
  );
  return {
    revision: group.revision,
    base: remote.values,
    values: {
      title: title.value,
      resolution: resolution.value,
      questionIds: archive ? [] : members.value,
    },
    conflicts: {
      title: title.conflict,
      resolution: resolution.conflict,
      questionIds: !archive && members.conflict,
    },
  };
}

export function chooseGroupEdit(
  edit: GroupEdit,
  field: EditField,
  server: boolean,
): GroupEdit {
  return {
    ...edit,
    values: {
      ...edit.values,
      [field]: server ? edit.base[field] : edit.values[field],
    },
    conflicts: { ...edit.conflicts, [field]: false },
  };
}
