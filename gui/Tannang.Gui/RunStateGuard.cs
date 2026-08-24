// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

namespace Tannang.Gui;

public sealed class RunStateGuard
{
    private int _running;

    public bool IsRunning => Volatile.Read(ref _running) == 1;

    public bool TryEnter()
    {
        return Interlocked.CompareExchange(ref _running, 1, 0) == 0;
    }

    public void Exit()
    {
        Volatile.Write(ref _running, 0);
    }
}
