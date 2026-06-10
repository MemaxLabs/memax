import type { Memory } from "@/hooks/use-memories";
import type { RememberState } from "@/components/bar/remember-row";
import type { BarNotification, StagedFile } from "@/lib/bar-types";

export interface BarDomainState {
  recallQuery: string;
  askAnswer: string;
  aiLoading: boolean;
  aiStreaming: boolean;
  rememberState: RememberState;
  rememberedTitle: string;
  selectedMemory: Memory | null;
  barNotification: BarNotification | null;
  dreamNotification: BarNotification | null;
  statusNotification: BarNotification | null;
  stagedFiles: StagedFile[];
  pushing: boolean;
}

export type BarDomainAction =
  | { type: "setRecallQuery"; value: string }
  | { type: "setAskAnswer"; value: string }
  | { type: "setAiLoading"; value: boolean }
  | { type: "setAiStreaming"; value: boolean }
  | { type: "setRememberState"; value: RememberState }
  | { type: "setRememberedTitle"; value: string }
  | { type: "setSelectedMemory"; value: Memory | null }
  | { type: "setBarNotification"; value: BarNotification | null }
  | { type: "setDreamNotification"; value: BarNotification | null }
  | { type: "setStatusNotification"; value: BarNotification | null }
  | { type: "setStagedFiles"; value: StagedFile[] }
  | {
      type: "updateStagedFiles";
      updater: (files: StagedFile[]) => StagedFile[];
    }
  | { type: "setPushing"; value: boolean }
  | { type: "resetRemember" }
  | { type: "resetRecall" }
  | { type: "resetDomain" };

export function createInitialBarDomainState(): BarDomainState {
  return {
    recallQuery: "",
    askAnswer: "",
    aiLoading: false,
    aiStreaming: false,
    rememberState: "idle",
    rememberedTitle: "",
    selectedMemory: null,
    barNotification: null,
    dreamNotification: null,
    statusNotification: null,
    stagedFiles: [],
    pushing: false,
  };
}

export function barDomainReducer(
  state: BarDomainState,
  action: BarDomainAction,
): BarDomainState {
  switch (action.type) {
    case "setRecallQuery":
      return { ...state, recallQuery: action.value };
    case "setAskAnswer":
      return { ...state, askAnswer: action.value };
    case "setAiLoading":
      return { ...state, aiLoading: action.value };
    case "setAiStreaming":
      return { ...state, aiStreaming: action.value };
    case "setRememberState":
      return { ...state, rememberState: action.value };
    case "setRememberedTitle":
      return { ...state, rememberedTitle: action.value };
    case "setSelectedMemory":
      return { ...state, selectedMemory: action.value };
    case "setBarNotification":
      return { ...state, barNotification: action.value };
    case "setDreamNotification":
      return { ...state, dreamNotification: action.value };
    case "setStatusNotification":
      return { ...state, statusNotification: action.value };
    case "setStagedFiles":
      return { ...state, stagedFiles: action.value };
    case "updateStagedFiles":
      return { ...state, stagedFiles: action.updater(state.stagedFiles) };
    case "setPushing":
      return { ...state, pushing: action.value };
    case "resetRemember":
      return {
        ...state,
        rememberState: "idle",
        rememberedTitle: "",
      };
    case "resetRecall":
      return {
        ...state,
        recallQuery: "",
        askAnswer: "",
        aiLoading: false,
        aiStreaming: false,
      };
    case "resetDomain":
      return {
        ...createInitialBarDomainState(),
        dreamNotification: state.dreamNotification,
        statusNotification: state.statusNotification,
      };
    default:
      return state;
  }
}
