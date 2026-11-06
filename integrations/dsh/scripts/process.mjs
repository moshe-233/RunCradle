/** Stop only a detached process group created by this test run; require proven cleanup. */
export async function stopOwnedProcess(child, closed, { timeoutMs = 10000, completed }) {
  const kill = (signal) => {
    if (child.pid) try { process.kill(-child.pid, signal); }
    catch (error) { if (error.code !== 'ESRCH') throw error; }
  };
  if (child.exitCode === null && child.signalCode === null) kill('SIGTERM');
  let forced = false;
  const force = setTimeout(() => {
    forced = true;
    kill('SIGKILL');
  }, timeoutMs);
  try {
    const result = await closed;
    if (forced) throw new Error('Shutdown required forced termination');
    if (result.code !== 0 || result.signal) throw new Error(`Unclean shutdown exit: ${JSON.stringify(result)}`);
    if (!completed()) throw new Error('Shutdown missing cleanup acknowledgement');
    return result;
  }
  finally { clearTimeout(force); }
}

/** Own every detached test child, including on Ctrl+C or an outer runner's SIGTERM. */
export function createProcessOwner({ timeoutMs = 10000, onInterrupt = async () => {} } = {}) {
  const children = new Map();
  let interrupted = false, finished;
  function interrupt(signal) {
    if (interrupted) return;
    interrupted = true;
    const owned = [...children];
    const kill = (termination) => {
      for (const [child] of owned) {
        if (child.pid) try { process.kill(-child.pid, termination); }
        catch (error) { if (error.code !== 'ESRCH') process.stderr.write(`${error.message}\n`); }
      }
    };
    finished = (async () => {
      kill('SIGTERM');
      const force = setTimeout(() => kill('SIGKILL'), timeoutMs);
      try { await Promise.allSettled(owned.map(([, settled]) => settled)); }
      finally { clearTimeout(force); }
      await onInterrupt(signal);
    })().finally(() => process.exit(signal === 'SIGINT' ? 130 : 143));
  }
  const onInt = () => interrupt('SIGINT');
  const onTerm = () => interrupt('SIGTERM');
  process.on('SIGINT', onInt);
  process.on('SIGTERM', onTerm);
  return {
    get interrupted() { return interrupted; },
    get finished() { return finished; },
    assertRunning() { if (interrupted) throw new Error('Test run interrupted'); },
    track(child, settled) {
      children.set(child, settled);
      void settled.then(() => children.delete(child), () => children.delete(child));
    },
    close() {
      process.off('SIGINT', onInt);
      process.off('SIGTERM', onTerm);
    },
  };
}
