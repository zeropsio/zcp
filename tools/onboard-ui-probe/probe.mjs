import puppeteer from "/Users/macbook/Documents/Zerops-MCP/zcp/tools/welcome-bridge-harness/node_modules/puppeteer-core/lib/esm/puppeteer/puppeteer-core.js";
const EMAIL=process.env.ZE_EMAIL, PASS=process.env.ZE_PASS, PROJECT="gRLfpBNrSziMKj0VEfk6vw";
const AGENT=process.env.AGENT||"codex";
const DO_LAUNCH=process.env.DO_LAUNCH==="1";
const sleep=(ms)=>new Promise(r=>setTimeout(r,ms));
const browser=await puppeteer.launch({channel:"chrome",headless:true,args:["--window-size=1600,1000"],defaultViewport:{width:1600,height:1000}});
try{
  const page=await browser.newPage();
  await page.goto("http://localhost:1111/",{waitUntil:"domcontentloaded",timeout:60000}); await sleep(4000);
  if(await page.$('input[type="email"]')){
    await page.type('input[type="email"]',EMAIL,{delay:5}); await page.type('input[type="password"]',PASS,{delay:5});
    await page.evaluate(()=>{const b=[...document.querySelectorAll("button")].find(x=>x.textContent.includes("Login using email")); if(b)b.click();});
    await sleep(6000);
    const t=await page.evaluate(()=>document.body.innerText);
    if(t.includes("Choose your organization")){
      await page.evaluate(()=>{const els=[...document.querySelectorAll('button,a,[role="button"],div,span')].filter(x=>{const s=(x.textContent||"").trim();return s.startsWith("KRLS")&&!s.includes("AWESOME")&&s.length<40&&x.getBoundingClientRect().height>0;}).sort((a,b)=>a.textContent.length-b.textContent.length); if(els[0])els[0].click();});
      await sleep(5000);
    }
  }
  await page.goto(`http://localhost:1111/project/${PROJECT}?zcpOnboard=1`,{waitUntil:"domcontentloaded",timeout:60000});
  await sleep(600);
  // dev entry raises the claiming cover first, then enters picking once userData resolves (§8.2)
  await page.screenshot({path:"shot-claiming.png"});
  for(let i=0;i<90;i++){ if(await page.evaluate(()=>(document.querySelector("z-zcp-onboard-wizard")?.querySelectorAll(".__agent-btn").length||0)>0)) break; await sleep(100); }
  await sleep(400);
  await page.screenshot({path:"shot-picker.png"});
  const tiles=await page.evaluate(()=>{
    const btns=[...document.querySelectorAll("z-zcp-onboard-wizard .__agent-btn")];
    return {count:btns.length,
      pressed:btns.map(b=>b.getAttribute("aria-pressed")),
      badges:btns.filter(b=>b.querySelector(".__badge")).length,
      restBorder:btns[0]?getComputedStyle(btns[0]).borderTopWidth:null};
  });
  // dark-theme snapshot of the picker (same class convention as :host-context in the component)
  await page.evaluate(()=>{document.documentElement.classList.add("zef-dark-theme");});
  await sleep(250);
  await page.screenshot({path:"shot-picker-dark.png"});
  await page.evaluate(()=>{document.documentElement.classList.remove("zef-dark-theme");});
  await sleep(250);
  // hover the 2nd tile - and prove the row is geometrically still under the cursor
  const before=await page.evaluate(()=>{const b=document.querySelectorAll("z-zcp-onboard-wizard .__agent-btn")[1]; const r=b.getBoundingClientRect(); return {x:r.x,y:r.y,w:r.width,h:r.height};});
  await page.mouse.move(before.x+before.w/2,before.y+before.h/2); await sleep(500);
  const after=await page.evaluate(()=>{const b=document.querySelectorAll("z-zcp-onboard-wizard .__agent-btn")[1]; const r=b.getBoundingClientRect(); return {x:r.x,y:r.y,w:r.width,h:r.height};});
  const hoverMoved=Math.abs(before.x-after.x)+Math.abs(before.y-after.y)+Math.abs(before.w-after.w)+Math.abs(before.h-after.h)>0.5;
  await page.screenshot({path:"shot-hover.png"});
  // pick the target agent
  await page.evaluate((a)=>{const b=[...document.querySelectorAll("z-zcp-onboard-wizard .__agent-btn")].find(x=>new RegExp(a,"i").test(x.textContent)); if(b)b.click();},AGENT);
  await sleep(3500);
  await page.screenshot({path:"shot-after-pick.png"});
  const read=()=>page.evaluate(()=>{
    const w=document.querySelector("z-zcp-onboard-wizard");
    const inner=w?.firstElementChild;
    const pane=document.querySelector(".cdk-overlay-pane");
    const dlgVisible = pane ? (()=>{const r=pane.getBoundingClientRect(); const el=document.elementFromPoint(r.x+r.width/2, r.y+r.height/2); return !!el && pane.contains(el);})() : false;
    const btns=[...(w?.querySelectorAll(".__agent-btn")||[])];
    const selectedIdx=btns.findIndex(b=>b.getAttribute("aria-pressed")==="true");
    const cta=[...(w?.querySelectorAll("button")||[])].find(b=>/start onboarding/i.test(b.textContent||""));
    return {wizardText:(w?.innerText||"(none)").replace(/\s+/g," ").slice(0,140),
      wizardZ:inner?getComputedStyle(inner).zIndex:null,
      dialogPanePresent:!!pane, dialogOnTop:dlgVisible,
      tileCount:btns.length, selectedIdx,
      focusOnSelected:selectedIdx>=0&&document.activeElement===btns[selectedIdx],
      ctaPresent:!!cta, ctaFocused:!!cta&&document.activeElement===cta};
  });
  let afterPick=await read();
  const result={tiles,hoverMoved,afterPick};
  if(afterPick.dialogPanePresent){
    // auth path: dismiss (ESC, then an explicit close affordance if the pane survives)
    // and prove the wizard bounced back to picking with the pick retained + focused
    await page.keyboard.press("Escape"); await sleep(1000);
    if(await page.$(".cdk-overlay-pane")){
      await page.evaluate(()=>{const c=[...document.querySelectorAll('.cdk-overlay-pane button,.cdk-overlay-pane [role="button"]')].find(b=>/close|zavřít|×/i.test((b.getAttribute("aria-label")||"")+(b.textContent||""))); if(c)c.click();});
      await sleep(1000);
    }
    await page.screenshot({path:"shot-after-dismiss.png"});
    result.afterDismiss=await read();
  } else if(afterPick.ctaPresent){
    // authorized skip: launch-ready confirmation gate (§8.1)
    await page.screenshot({path:"shot-launch-ready.png"});
    if(DO_LAUNCH){
      await page.evaluate(()=>{const b=[...document.querySelectorAll("z-zcp-onboard-wizard button")].find(x=>/start onboarding/i.test(x.textContent||"")); if(b)b.click();});
      await sleep(2000);
      await page.screenshot({path:"shot-launching.png"});
      result.afterLaunch=await read();
    }
  }
  console.log("VISUAL "+JSON.stringify(result,null,2));
} finally { await browser.close().catch(()=>{}); }
