import {work as run} from './lib';
const local = {
  method() { run(); },
  fn: () => run(),
};
export function entry() { run(); local.method(); local.fn(); }
export function shadow() { function run() {} run(); }
const {method: invoke} = local;
invoke();
