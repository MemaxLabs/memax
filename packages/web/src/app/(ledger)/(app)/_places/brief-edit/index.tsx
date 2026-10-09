"use client";

import { Button } from "@memaxlabs/ledger";
import { interpolate } from "@/i18n";
import { HeaderSkeleton, PlaceSkeleton } from "../../_components/skeleton";
import { PlaceError } from "../../_components/status";
import { StatusPage } from "../../../_components/status-page";
import { useBrief, useTargets } from "../../_lib/compile";
import { briefHref } from "../brief";
import { BriefEmpty } from "../brief/brief-empty";
import { useRecordsView } from "../records-view";
import { BriefEditor } from "./editor";
import styles from "../brief/brief.module.css";

/**
 * /[space]/brief/edit (BriefEdit.png, epic 1.5): the Brief's draft, kept
 * as a new version when the person presses Done. Viewers propose, so
 * they can't edit; a space with no Brief starts one first.
 */
export function BriefEditPlace() {
  const view = useRecordsView();
  const { space, l, copy } = view;
  const brief = useBrief(space);
  const targets = useTargets(space);

  if (space.role === "viewer") {
    return (
      <StatusPage
        variant="sheet"
        title={l.brief.page.editViewer}
        description={l.brief.edit.viewerDetail}
        actions={
          <Button variant="secondary" size="sm" href={briefHref(space.slug)}>
            {l.brief.target.notFound.open}
          </Button>
        }
      />
    );
  }
  if (brief.data === undefined) {
    if (brief.isError) {
      return <PlaceError space={space} onRetry={() => void brief.refetch()} />;
    }
    return (
      <div className={`mx-page ${styles.page}`}>
        <HeaderSkeleton />
        <PlaceSkeleton
          title={l.brief.edit.bar}
          status={copy.empty.loadingMeta}
          label={interpolate(copy.empty.loading, { place: l.brief.edit.bar })}
        />
      </div>
    );
  }
  if (brief.data === null) return <BriefEmpty view={view} />;
  // The draft starts from the version first loaded and keeps its edits
  // if a newer one arrives meanwhile (Done then says so and rebases).
  return <BriefEditor view={view} brief={brief.data} targets={targets.data} />;
}
