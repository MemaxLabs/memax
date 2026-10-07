import { ICON_NAMES, Icon } from "../brand/icon";
import { Logo } from "../brand/logo";
import { Seal } from "../brand/seal";
import { PreviewFrame, type PreviewProps } from "./frame";

// Lockup geometry in its own units (see LOCKUP in brand/logo.tsx): ink height 780,
// cap 750, baseline at 765, mark ink 998 wide, gap 225.
const S = 64 / 780;
const px = (units: number) => `${(units * S).toFixed(1)}px`;

function Construction() {
  return (
    <div className="pv-con">
      <div className="pv-con-art">
        <span className="pv-g" style={{ top: px(15) }} />
        <span className="pv-g" style={{ top: px(765) }} />
        <span className="pv-gap" style={{ left: px(998), width: px(225) }} />
        <Logo variant="lockup" size={64} />
      </div>
      <dl className="pv-con-notes">
        <div>
          <dt>Mark</dt>
          <dd>1.04× the cap height, centred on it</dd>
        </div>
        <div>
          <dt>Gap</dt>
          <dd>0.30× the cap height, mark to serif</dd>
        </div>
        <div>
          <dt>Name</dt>
          <dd>Gloock, −0.015em, outlined</dd>
        </div>
      </dl>
    </div>
  );
}

export function LogoPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="logo" {...props}>
      <div className="mx-stage pv-col">
        <div className="pv-row">
          <Logo variant="lockup" size={36} />
          <Logo variant="lockup" size={20} />
          <Logo variant="mark" size={40} />
          <span className="pv-inv">
            <Logo variant="lockup" size={22} />
          </span>
        </div>
        <Construction />
      </div>
    </PreviewFrame>
  );
}

export function SealPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="seal" {...props}>
      <div className="mx-stage pv-row">
        <Seal date="Oct 2" id="M-0219" size={136} />
        <Seal date="Oct 5" id="M-0430" size={76} />
        <div className="pv-inrow">
          <span className="pv-txt">
            Background jobs run on River, not Temporal.
          </span>
          <Seal date="Oct 2" size={22} />
        </div>
      </div>
    </PreviewFrame>
  );
}

export function IconPreview(props: PreviewProps) {
  return (
    <PreviewFrame name="icon" {...props}>
      <div className="mx-stage pv-grid">
        {ICON_NAMES.map((name) => (
          <div key={name} className="pv-ic">
            <Icon name={name} size={20} />
            <span>{name}</span>
          </div>
        ))}
      </div>
    </PreviewFrame>
  );
}
