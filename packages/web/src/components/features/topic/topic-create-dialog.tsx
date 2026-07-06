"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  Input,
  Button,
} from "@memaxlabs/ui";
import { useLocale, useInterpolate } from "@/i18n";
import { useCreateTopic } from "@/hooks/use-topics";

/**
 * TopicCreateDialog — centered glass dialog for creating a topic or a
 * subtopic. Name-only on purpose: icon defaults to "folder" and the
 * dream engine refines description/icon later, so the create moment
 * stays one keystroke away from done (zero-learning-curve rule).
 *
 * `parent` carries the subtopic case: { id, name } scopes the create
 * under that node and titles the dialog "New subtopic in {name}".
 */
export interface TopicCreateTarget {
  parent?: { id: string; name: string };
}

export function TopicCreateDialog({
  target,
  onClose,
  onCreated,
}: {
  /** null = closed. */
  target: TopicCreateTarget | null;
  onClose: () => void;
  /** Called with the new topic id after a successful create. */
  onCreated?: (topicId: string) => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const createTopic = useCreateTopic();
  const [name, setName] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);
  const open = target !== null;

  // Fresh name per open — the dialog is reused across targets.
  useEffect(() => {
    if (open) {
      setName("");
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open, target]);

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed || createTopic.isPending) return;
    createTopic.mutate(
      { name: trimmed, parent_id: target?.parent?.id },
      {
        onSuccess: (topic) => {
          onClose();
          onCreated?.(topic.id);
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-xs">
        <DialogHeader>
          <DialogTitle>
            {target?.parent
              ? interpolate(t.topics.createSubtopicTitle, {
                  name: target.parent.name,
                })
              : t.topics.createTitle}
          </DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex flex-col gap-3">
          <Input
            ref={inputRef}
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={t.topics.createNamePlaceholder}
            maxLength={100}
            aria-label={t.topics.createNamePlaceholder}
          />
          <div className="flex items-center justify-end gap-2">
            <Button type="button" variant="ghost" size="sm" onClick={onClose}>
              {t.topics.createCancel}
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={!name.trim() || createTopic.isPending}
            >
              {createTopic.isPending
                ? t.topics.creating
                : t.topics.createSubmit}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
