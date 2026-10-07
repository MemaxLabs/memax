import { configure } from "@testing-library/dom";

// findBy* and waitFor give up after 1 s by default. A refetch and a
// re-render can take longer than that on a loaded machine (CI, or a full
// run beside other test processes), which made DOM tests fail at random.
// 5 s still fails a real hang quickly.
configure({ asyncUtilTimeout: 5000 });
